package datasources

import (
	"os"
	"path/filepath"

	"github.com/BaizeAI/dataset/pkg/log"
	"github.com/BaizeAI/dataset/pkg/utils"
)

type Secrets struct {
	Username string `json:"-"`
	Password string `json:"-"`

	SSHPrivateKey           string `json:"-"`
	SSHPrivateKeyPassphrase string `json:"-"`

	Token string `json:"-"`

	AKSKAccessKeyID     string `json:"-"`
	AKSKSecretAccessKey string `json:"-"`
}

var (
	keys = []utils.SecretKey{
		utils.SecretKeyUsername,
		utils.SecretKeyPassword,
		utils.SecretKeyPrivateKey,
		utils.SecretKeyPrivateKeyPassphrase,
		utils.SecretKeyToken,
		utils.SecretKeyAccessKey,
		utils.SecretKeySecretKey,
	}
)

func ReadAndParseSecrets(name string) (Secrets, error) {
	mSecrets := make(map[utils.SecretKey]string)

	logger := log.WithField("secretMountDir", name)

	for _, v := range keys {
		secretContent, err := os.ReadFile(filepath.Join(name, string(v)))
		if err != nil {
			logger.WithField("secretDataKey", v).Debug("failed to read secret")
			continue
		}

		mSecrets[v] = string(secretContent)
	}

	return Secrets{
		Username:                mSecrets[utils.SecretKeyUsername],
		Password:                mSecrets[utils.SecretKeyPassword],
		SSHPrivateKey:           mSecrets[utils.SecretKeyPrivateKey],
		SSHPrivateKeyPassphrase: mSecrets[utils.SecretKeyPrivateKeyPassphrase],
		Token:                   mSecrets[utils.SecretKeyToken],
		AKSKAccessKeyID:         mSecrets[utils.SecretKeyAccessKey],
		AKSKSecretAccessKey:     mSecrets[utils.SecretKeySecretKey],
	}, nil
}
