### MySQL Backup to S3 using Kubernetes Cronjobs
* Create S3 bucket
* Create IAM user
* Store the Access Key ID and Secret Key of the IAM user
* Provide S3 bucket full access to the IAM user
```
{
    "Version": "2012-10-17",
    "Statement": [
        {
            "Effect": "Allow",
            "Action": [
                "s3:PutObject",
                "s3:GetObject",
                "s3:DeleteObject"
            ],
            "Resource": [
                "arn:aws:s3:::<bucket_name>/*"
            ]
        }
    ]
}
```


* Find the MySQL root password
* Create Kubernetes Secrets for the below
    * MySQL root password 
        * `kubectl create secret generic mysql-pass --from-literal=password=<root_password>`
    * AWS Access Key ID
        * `kubectl create secret generic s3-access --from-literal=AWS_ACCESS_KEY_ID=<ACCESS_KEY_ID>`
    * AWS Secret Access Key
        * `echo -n ‘<SECRET_ACCESS_KEY>’ | base64`
        * `copy the output from above echo command`
        * `kubectl edit secret s3-access`
        * Add `AWS_SECRET_ACCESS_KEY: <paste_output_of_echo>` under data section
        
* Download the YAML file from this link
`https://github.com/mattermost/mattermost-kubernetes/blob/master/mysql-backup/mysql-dump-ScheduledJob.yaml`
* Modify DB_NAMES environment variable from the YAML file
* Replace the below values,
    * DB_DUMP_TARGET - S3 bucket created above
    * DB_SERVER - Service name of the MySql deployment/statefulset
    * DB_USER - root
    * DB_PASS - secretKeyRef name: mysql-pass, secretKeyRef key: password
    * AWS_ACCESS_KEY_ID - secretKeyRef name: s3-access, secretKeyRef key: AWS_ACCESS_KEY_ID
    * AWS_SECRET_ACCESS_KEY- secretKeyRef name: s3-access, secretKeyRef key: AWS_SECRET_ACCESS_KEY
    * AWS_REGION - Region where your services deployed
    * schedule: "0 0 * * *" - Change this cron for when to execute the backup job
* Once done with replacing all the values deploy the cronjob into the cluster
    * `kubectl apply -f mysql-dump-ScheduledJob.yaml -n <NAMESPACE>`


### Restore Process
* Download this YAML file
    * `https://github.com/mattermost/mattermost-kubernetes/blob/master/mysql-backup/mysql-restore-Job.yaml`
* Modify DB_NAMES environment variable from the YAML file
* Replace the values as same as backup process.
* Deploy the job into the cluster, to start the recovery process. Note: This will replace all the data inside mysql and restore it from backup.
    * `kubectl apply -f mysql-restore-Job.yaml -n <NAMESPACE>`
