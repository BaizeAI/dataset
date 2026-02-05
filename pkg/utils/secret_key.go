package utils

type SecretKey string

// external code needs to reference the following fields, which were previously placed in the internal directory
// and therefore couldn't be accessed by external code.
const (
	SecretKeyUsername             SecretKey = "username"
	SecretKeyPassword             SecretKey = "password"
	SecretKeyPrivateKey           SecretKey = "ssh-privatekey"
	SecretKeyPrivateKeyPassphrase SecretKey = "ssh-privatekey-passphrase" // #nosec G101
	SecretKeyToken                SecretKey = "token"
	SecretKeyAccessKey            SecretKey = "access-key"
	SecretKeySecretKey            SecretKey = "secret-key"
)
