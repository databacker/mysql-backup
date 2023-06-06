#!/bin/bash
set -ex

source ./_functions.sh

#cleanup all temporary folders
mkdir -p backups
rm -rf backups/*.tgz
mkdir -p certs
rm -rf certs/*.pem

makevolume

makenetwork

make_test_images

makesmb

start_service_containers

#temporary use original certificate location
MYSQLDUMP_OPTS="--ssl-cert /var/lib/mysql/client-cert.pem --ssl-key /var/lib/mysql/client-key.pem"
db_connect="docker exec -i $mysql_cid mysql ${MYSQLDUMP_OPTS} -u$MYSQLUSER -p$MYSQLPW --protocol=tcp -h127.0.0.1 --wait --connect_timeout=20 tester"
$db_connect -e 'select 1;'
echo 'use tester; create table t1 (id INT, name VARCHAR(20)); INSERT INTO t1 (id,name) VALUES (1, "John"), (2, "Jill"), (3, "Sam"), (4, "Sarah");' | $db_connect


#fix /certs/*.pem files permissions
c_with_wrong_permission=$(docker container create --label mysqltest --net mysqltest --name mysqltest-fix-certs-permissions -v ${BACKUP_VOL}:/backups -v ${CERTS_VOL}:/certs ${DBDEBUG} -e DB_USER=$MYSQLUSER -e DB_PASS=$MYSQLPW -e DB_DUMP_FREQ=60 -e DB_DUMP_BEGIN=+0 -e DB_SERVER=mysql -e MYSQLDUMP_OPTS="--compact ${MYSQLDUMP_OPTS}" ${BACKUP_IMAGE})
docker container start ${c_with_wrong_permission} >/dev/null
docker exec -u 0 ${c_with_wrong_permission} chown -R appuser /certs>/dev/null
rm_containers $c_with_wrong_permission

# now we can reset to /certs
MYSQLDUMP_OPTS="--ssl-cert /certs/client-cert.pem --ssl-key /certs/client-key.pem"

# copy our certificates locally
docker cp mysql:/certs/client-key.pem $PWD/certs/client-key.pem
docker cp mysql:/certs/client-cert.pem $PWD/certs/client-cert.pem

cid_dump=$( \
docker container create \
-u 0 \
--label mysqltest \
--name mysqldump \
--net mysqltest \
-v $PWD/backups/:/backups \
-v $PWD/certs/client-key.pem:/certs/client-key.pem \
-v $PWD/certs/client-cert.pem:/certs/client-cert.pem \
-e DB_DUMP_TARGET=/backups \
-e DB_DUMP_DEBUG=0 \
-e DB_SERVER=mysql \
-e DB_USER=$MYSQLUSER \
-e DB_PASS=$MYSQLPW \
-e RUN_ONCE=1 \
-e MYSQLDUMP_OPTS="${MYSQLDUMP_OPTS}" \
${BACKUP_IMAGE})
docker container start $cid_dump

sleepwait 5

# remove tester database so we can be sure that restore actually works restoring tester database
docker exec -i mysql mysql ${MYSQLDUMP_OPTS} -uroot -proot --protocol=tcp -h127.0.0.1 -e "drop database tester;"

ls -l $PWD/backups
backup_name=$(ls $PWD/backups)
if [[ "$backup_name" == "" ]]; then
  echo "***********************************"
  echo "backup file was not created, see container log output:"
  docker logs $cid_dump
  echo "***********************************"
  exit 1
fi

cid_restore=$( \
docker container create \
-u 0 \
--label mysqltest \
--name mysqlrestore \
--net mysqltest \
-v $PWD/certs/client-key.pem:/certs/client-key.pem \
-v $PWD/certs/client-cert.pem:/certs/client-cert.pem \
-v $PWD/backups/:/backups \
-e DB_DUMP_DEBUG=0 \
-e DB_SERVER=mysql \
-e DB_USER=$MYSQLUSER \
-e DB_PASS=$MYSQLPW \
-e DB_RESTORE_TARGET=/backups/${backup_name} \
-e RESTORE_OPTS="${RESTORE_OPTS}" \
${BACKUP_IMAGE})
docker container start $cid_restore

sleepwait 5

db_command="docker exec -i $mysql_cid mysql ${MYSQLDUMP_OPTS} -u$MYSQLUSER -p$MYSQLPW --protocol=tcp -h127.0.0.1 tester"
echo 'use tester; select * from t1' | $db_command

rm_service_containers $smb_cid $mysql_cid $s3_cid
docker rm $cid_dump $cid_restore

rm -rf backups/*.tgz
rmdir backups
rm -rf certs/*.pem
rmdir certs

