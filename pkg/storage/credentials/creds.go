package credentials

type Creds struct {
	SMB SMBCreds
	AWS AWSCreds
}

type SMBCreds struct {
	Username string
	Password string
	Domain   string
}

type AWSCreds struct {
	AccessKeyID     string
	SecretAccessKey string
	Endpoint        string
	Region          string
	S3UsePathStyle  bool
}
