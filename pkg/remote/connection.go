package remote

type Connection struct {
	URL string `yaml:"url"`
	// Certificate digests of the certificate of the remote server or one that signed it in the chain.
	// Value starts with the hash algorithm (e.g. sha256:) followed by the digest.
	// Only known listed algorithms are supported; others are considered an error.
	Certificates []string `yaml:"certificates"` // e.g. sha256:69729b8e15a86efc177a57afb7171dfc64add28c2fca8cf1507e34453ccb1470
	// Credentials to use to authenticate to the remote server.
	// Format of the credentials is base64-encoded Curve25519 private key.
	Credentials string `yaml:"credentials"` // e.g. BwMqVfr1myxqX8tikIPYCyNtpHgMLIg/2nUE+pLQnTE=
}
