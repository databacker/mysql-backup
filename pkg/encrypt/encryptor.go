package encrypt

import (
	"fmt"
	"io"

	"github.com/databacker/api/go/api"
)

type Encryptor interface {
	Name() string
	Description() string
	Decrypt(out io.Writer) (io.WriteCloser, error)
	Encrypt(out io.Writer) (io.WriteCloser, error)
}

func GetEncryptor(name string, key []byte) (Encryptor, error) {
	var (
		enc Encryptor
		err error
	)
	nameEnum := api.EncryptionAlgorithm(name)
	switch nameEnum {
	case AlgoSMimeAES256CBC:
		enc, err = NewSMimeAES256CBC(key)
	case AlgoDirectAES256CBC:
		enc, err = NewAES256CBC(key, nil, true)
	case AlgoPBKDF2AES256CBC:
		enc, err = NewPBKDF2AES256CBC(key)
	case AlgoAgeChacha20Poly1305:
		enc, err = NewAgeChacha20Poly1305(key)
	case AlgoChacha20Poly1305:
		enc, err = NewChacha20Poly1305(key)
	default:
		return nil, fmt.Errorf("unknown encryption format: %s", name)
	}
	return enc, err
}
