package credentials

type Creds struct {
	SMBCredentials SMBCreds
	AWSEndpoint    string
}

type SMBCreds struct {
	Username string
	Password string
	Domain   string
}
