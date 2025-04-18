package encrypt

import "github.com/databacker/api/go/api"

const (
	AlgoSMimeAES256CBC      = api.EncryptionAlgorithmSmimeAes256Cbc
	AlgoDirectAES256CBC     = api.EncryptionAlgorithmAes256Cbc
	AlgoPBKDF2AES256CBC     = api.EncryptionAlgorithmPbkdf2Aes256Cbc
	AlgoAgeChacha20Poly1305 = api.EncryptionAlgorithmAgeChacha20Poly1305
	AlgoChacha20Poly1305    = api.EncryptionAlgorithmChacha20Poly1305
)

var All = []string{
	string(AlgoSMimeAES256CBC),
	string(AlgoDirectAES256CBC),
	string(AlgoPBKDF2AES256CBC),
	string(AlgoAgeChacha20Poly1305),
	string(AlgoChacha20Poly1305),
}
