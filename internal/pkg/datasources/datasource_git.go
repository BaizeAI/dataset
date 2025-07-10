package datasources

import (
	"bytes"
	"crypto"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/samber/lo"
	"github.com/sirupsen/logrus"

	"golang.org/x/crypto/ssh"

	"github.com/BaizeAI/dataset/pkg/log"
	"github.com/BaizeAI/dataset/pkg/utils"
)

var _ Loader = &GitLoader{}

type GitLoader struct {
	Options Options

	gitOptions GitLoaderOptions
}

func NewGitLoader(datasourceOption map[string]string, options Options, secrets Secrets) (*GitLoader, error) {
	git := new(GitLoader)
	gitOptions, err := git.gitOptions.parseOptionsFromOptions(datasourceOption)
	if err != nil {
		return nil, err
	}

	git.Options = options
	git.gitOptions = gitOptions
	git.gitOptions.username = strings.TrimSpace(secrets.Username)
	git.gitOptions.password = strings.TrimSpace(secrets.Password)
	git.gitOptions.sshPrivateKey = strings.TrimSpace(secrets.SSHPrivateKey)
	git.gitOptions.sshPrivateKeyPassphrase = strings.TrimSpace(secrets.SSHPrivateKeyPassphrase)
	git.gitOptions.token = strings.TrimSpace(secrets.Token)

	return git, nil
}

type GitLoaderOptions struct {
	Branch     string `json:"branch"`
	Commit     string `json:"commit"`
	Depth      string `json:"depth"`
	Submodules string `json:"submodules"`

	depth                   int64
	username                string
	password                string
	sshPrivateKey           string
	sshPrivateKeyPassphrase string
	sshPrivateKeyFullPath   string
	token                   string
}

func (d *GitLoader) secrets() []string {
	return []string{
		d.gitOptions.username,
		url.QueryEscape(d.gitOptions.username),
		d.gitOptions.password,
		url.QueryEscape(d.gitOptions.password),
		d.gitOptions.sshPrivateKey,
		d.gitOptions.sshPrivateKeyPassphrase,
		url.QueryEscape(d.gitOptions.token),
	}
}

func (d *GitLoaderOptions) parseOptionsFromOptions(options map[string]string) (GitLoaderOptions, error) {
	jsonContent, err := json.Marshal(options)
	if err != nil {
		return GitLoaderOptions{}, err
	}

	var gitOptions GitLoaderOptions
	err = json.Unmarshal(jsonContent, &gitOptions)
	if err != nil {
		return GitLoaderOptions{}, err
	}

	if gitOptions.Depth != "" {
		gitOptions.depth, err = strconv.ParseInt(gitOptions.Depth, 10, 64)
		if err != nil {
			return GitLoaderOptions{}, fmt.Errorf("failed to parse depth, err: %s", err)
		}
	}

	return gitOptions, nil
}

func preparePrivateKeyToSSHDir(sshPrivateKey, sshPrivateKeyPassphrase string) (string, error) {
	if sshPrivateKey == "" {
		return "", nil
	}

	homeDir, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("failed to get user home directory, err: %s", err)
	}

	sshDir := filepath.Join(homeDir, ".ssh")

	err = os.MkdirAll(sshDir, 0700)
	if err != nil {
		return "", fmt.Errorf("failed to create .ssh directory, err: %s", err)
	}

	privateKeyFileName := fmt.Sprintf("baize_data_loader_%s_id", utils.RandomHashString(8))
	sshPrivateKeyFullPath := filepath.Join(sshDir, privateKeyFileName)

	privateKeyBuffer := new(bytes.Buffer)

	if sshPrivateKeyPassphrase != "" {
		key, err := ssh.ParseRawPrivateKeyWithPassphrase([]byte(sshPrivateKey), []byte(sshPrivateKeyPassphrase))
		if err != nil {
			return sshPrivateKeyFullPath, fmt.Errorf("failed to parse ssh private key with passphrase, err: %s", err)
		}

		privateKey, ok := key.(crypto.PrivateKey)
		if !ok {
			return sshPrivateKeyFullPath, fmt.Errorf("failed to convert ssh private key to crypto.PrivateKey")
		}

		// marshal private key to string
		privateKeyBytes, err := ssh.MarshalPrivateKey(privateKey, "baize-data-loader")
		if err != nil {
			return sshPrivateKeyFullPath, fmt.Errorf("failed to marshal ssh private key, err: %s", err)
		}

		err = pem.Encode(privateKeyBuffer, privateKeyBytes)
		if err != nil {
			return sshPrivateKeyFullPath, fmt.Errorf("failed to encode ssh private key, err: %s", err)
		}
	} else {
		key, err := ssh.ParseRawPrivateKey([]byte(sshPrivateKey))
		if err != nil {
			return sshPrivateKeyFullPath, fmt.Errorf("failed to parse ssh private key, err: %s", err)
		}

		_, ok := key.(crypto.PrivateKey)
		if !ok {
			return sshPrivateKeyFullPath, fmt.Errorf("failed to convert ssh private key to crypto.PrivateKey")
		}

		privateKeyBuffer.WriteString(sshPrivateKey)
		privateKeyBuffer.WriteString("\n")
	}

	return sshPrivateKeyFullPath, os.WriteFile(sshPrivateKeyFullPath, privateKeyBuffer.Bytes(), 0600)
}

func (d *GitLoader) alterFromURIForToken(fromURI string, token string) string {
	parsedURL, err := url.Parse(fromURI)
	if err != nil {
		log.Debugf("failed to parse fromURI %s, err: %s", fromURI, err)
		return fromURI
	}
	if parsedURL.Scheme != "https" && parsedURL.Scheme != "http" {
		return fromURI
	}
	if token == "" {
		return fromURI
	}

	parsedURL.User = url.UserPassword("oauth2", token)
	return parsedURL.String()
}

func (d *GitLoader) alterFromURIForUsernameAndPasswordAccess(fromURI string, username string, password string) string {
	parsedURL, err := url.Parse(fromURI)
	if err != nil {
		log.Debugf("failed to parse fromURI %s, err: %s", fromURI, err)
		return fromURI
	}
	if parsedURL.Scheme != "https" && parsedURL.Scheme != "http" {
		return fromURI
	}
	if password == "" && username == "" {
		return fromURI
	}
	if username != "" && password != "" {
		parsedURL.User = url.UserPassword(username, password)
		return parsedURL.String()
	}

	parsedURL.User = url.User(username)
	return parsedURL.String()
}

func (d *GitLoader) checkoutCommit(logger *logrus.Entry, gitDir string) error {
	logger = logger.WithFields(logrus.Fields{
		"workingDirectory": gitDir,
	})

	args := []string{
		"checkout",
		d.gitOptions.Commit,
	}

	cmd := exec.Command("git", args...)
	cmd.Dir = gitDir
	cmd.Env = os.Environ()

	return utils.ExecuteCommand(logger, cmd, d.secrets())
}

func (d *GitLoader) updateIndex(logger *logrus.Entry, gitDir string) error {
	logger = logger.WithFields(logrus.Fields{
		"workingDirectory": gitDir,
	})

	args := []string{
		"update-index",
		"--refresh",
	}

	cmd := exec.Command("git", args...)
	cmd.Dir = gitDir
	cmd.Env = os.Environ()

	return utils.ExecuteCommand(logger, cmd, d.secrets())
}

func (d *GitLoader) addAll(logger *logrus.Entry, gitDir string) error {
	logger = logger.WithFields(logrus.Fields{
		"workingDirectory": d.Options.Root,
		"gitDirectory":     gitDir,
	})

	args := []string{
		"add",
		// Use -u since:
		// 1. -A or . will add untracked files, any may cause massive files to be loaded
		// to .git directory as fsck-able objects
		// 2. .Trash-0 like directories is not needed to be added when there is no
		// proper .gitignore configured globally, which may cause permission issues
		// when performing git commands
		//
		// For difference, see:
		// https://stackoverflow.com/a/26039014/19954520
		"-u",
	}

	cmd := exec.Command("git", args...)
	cmd.Dir = gitDir
	cmd.Env = os.Environ()

	return utils.ExecuteCommand(logger, cmd, d.secrets())
}

func (d *GitLoader) stashPendingChanges(logger *logrus.Entry, gitDir string) error {
	logger = logger.WithFields(logrus.Fields{
		"workingDirectory": gitDir,
	})

	args := []string{
		"stash",
	}

	cmd := exec.Command("git", args...)
	cmd.Dir = gitDir
	cmd.Env = os.Environ()

	return utils.ExecuteCommand(logger, cmd, d.secrets())
}

func (d *GitLoader) resetHardToTargetBranch(logger *logrus.Entry, gitDir string, branch string) error {
	logger = logger.WithFields(logrus.Fields{
		"workingDirectory": gitDir,
	})

	args := []string{
		"reset",
		"--hard",
	}
	if branch != "" {
		args = append(args, branch)
	} else {
		args = append(args, "origin/HEAD")
	}

	cmd := exec.Command("git", args...)
	cmd.Dir = gitDir
	cmd.Env = os.Environ()

	return utils.ExecuteCommand(logger, cmd, d.secrets())
}

func (d *GitLoader) remoteAddURL(logger *logrus.Entry, fromURI string, gitDir string, name string) error {
	logger = logger.WithFields(logrus.Fields{
		"workingDirectory": gitDir,
		"alteredFromURI":   utils.ObscureString(fromURI, d.secrets()),
		"remoteName":       name,
	})

	args := []string{
		"remote",
		"add",
		name,
		fromURI,
	}

	cmd := exec.Command("git", args...)
	cmd.Dir = gitDir
	cmd.Env = os.Environ()

	return utils.ExecuteCommand(logger, cmd, d.secrets())
}

func (d *GitLoader) remoteSetURL(logger *logrus.Entry, fromURI string, gitDir string, name string) error {
	logger = logger.WithFields(logrus.Fields{
		"workingDirectory": gitDir,
		"alteredFromURI":   utils.ObscureString(fromURI, d.secrets()),
		"remoteName":       name,
	})

	args := []string{
		"remote",
		"set-url",
		name,
		fromURI,
	}

	cmd := exec.Command("git", args...)
	cmd.Dir = gitDir
	cmd.Env = os.Environ()

	return utils.ExecuteCommand(logger, cmd, d.secrets())
}

func (d *GitLoader) remoteRemove(logger *logrus.Entry, gitDir string, name string) error {
	logger = logger.WithFields(logrus.Fields{
		"workingDirectory": gitDir,
		"remoteName":       name,
	})

	args := []string{
		"remote",
		"remove",
		name,
	}

	cmd := exec.Command("git", args...)
	cmd.Dir = gitDir
	cmd.Env = os.Environ()

	return utils.ExecuteCommand(logger, cmd, d.secrets())
}

func (d *GitLoader) configGlobalSetSafeDirectory(logger *logrus.Entry, gitDir string, directory string) error {
	logger = logger.WithFields(logrus.Fields{
		"workingDirectory": gitDir,
	})

	args := []string{
		"config",
		"--global",
		"safe.directory",
		directory,
	}

	cmd := exec.Command("git", args...)
	cmd.Dir = gitDir
	cmd.Env = os.Environ()

	return utils.ExecuteCommand(logger, cmd, d.secrets())
}

func (d *GitLoader) configSetFileMode(logger *logrus.Entry, gitDir string, mode bool) error {
	logger = logger.WithFields(logrus.Fields{
		"workingDirectory": gitDir,
	})

	args := []string{
		"config",
		"--local",
		"core.fileMode",
		fmt.Sprintf("%t", mode),
	}

	cmd := exec.Command("git", args...)
	cmd.Dir = gitDir
	cmd.Env = os.Environ()

	return utils.ExecuteCommand(logger, cmd, d.secrets())
}

func (d *GitLoader) clone(logger *logrus.Entry, alteredFromURI string, cloneToPath string) error {
	logger = logger.WithFields(logrus.Fields{
		"alteredFromURI":   utils.ObscureString(alteredFromURI, d.secrets()),
		"cloneToPath":      cloneToPath,
		"workingDirectory": d.Options.Root,
	})
	if d.gitOptions.sshPrivateKey != "" && d.gitOptions.sshPrivateKeyFullPath != "" {
		logger = logger.WithFields(logrus.Fields{
			"privateKeyFilePath": d.gitOptions.sshPrivateKeyFullPath,
		})
	}

	logger.Debugf("performing git clone command to replicate data served by git server")

	args := []string{
		"clone",
		alteredFromURI,
		cloneToPath,
	}
	if d.gitOptions.Branch != "" {
		args = append(args, "--branch", d.gitOptions.Branch)
	}
	if d.gitOptions.depth > 0 {
		args = append(args, "--depth", fmt.Sprintf("%d", d.gitOptions.depth))
	}
	if d.gitOptions.Submodules != "" {
		if d.gitOptions.depth > 0 {
			args = append(args, "--shallow-submodules")
		}

		args = append(args, "--recurse-submodules")
	}

	args = append(args, "-v")
	cmd := exec.Command("git", args...)
	cmd.Dir = d.Options.Root
	cmd.Env = os.Environ()
	if d.gitOptions.sshPrivateKey != "" {
		cmd.Env = append(cmd.Env, fmt.Sprintf("GIT_SSH_COMMAND=ssh -o StrictHostKeyChecking=no -i %s", d.gitOptions.sshPrivateKeyFullPath))
	}

	return utils.ExecuteCommand(logger, cmd, d.secrets())
}

func (d *GitLoader) branch(logger *logrus.Entry, forPath string) (string, error) {
	logger = logger.WithFields(logrus.Fields{
		"workingDirectory": forPath,
	})

	args := []string{
		"branch",
		"--show-current",
	}

	cmd := exec.Command("git", args...)
	cmd.Dir = forPath
	cmd.Env = os.Environ()

	outBuffer, err := utils.ExecuteCommandWithOutput(logger, cmd, d.secrets())
	if err != nil {
		return "", err
	}

	branch := strings.TrimSpace(outBuffer.String())
	outBuffer.Reset()

	return branch, nil
}

func (d *GitLoader) pull(logger *logrus.Entry, alteredFromURI string, pullForPath string, remoteName string) error {
	logger = logger.WithFields(logrus.Fields{
		"alteredFromURI":   utils.ObscureString(alteredFromURI, d.secrets()),
		"pullForPath":      pullForPath,
		"workingDirectory": pullForPath,
	})
	if d.gitOptions.sshPrivateKey != "" && d.gitOptions.sshPrivateKeyFullPath != "" {
		logger = logger.WithFields(logrus.Fields{
			"privateKeyFilePath": d.gitOptions.sshPrivateKeyFullPath,
		})
	}

	logger.Debugf("performing git pull command to replicate data served by git server")

	args := []string{
		"pull",
		remoteName,
	}
	if d.gitOptions.Branch != "" {
		args = append(args, d.gitOptions.Branch)
	} else {
		currentBranch, err := d.branch(logger, pullForPath)
		if err != nil {
			return err
		}

		args = append(args, currentBranch)
	}

	args = append(args, "-v")
	cmd := exec.Command("git", args...)
	cmd.Dir = pullForPath
	cmd.Env = os.Environ()
	if d.gitOptions.sshPrivateKey != "" {
		cmd.Env = append(cmd.Env, fmt.Sprintf("GIT_SSH_COMMAND=ssh -o StrictHostKeyChecking=no -i %s", d.gitOptions.sshPrivateKeyFullPath))
	}

	return utils.ExecuteCommand(logger, cmd, d.secrets())
}

func (d *GitLoader) configBeforeOperations(logger *logrus.Entry, finalizedGitDir string) error {
	// Since data-loader should always be run as root (uid: 0, gid: 0),
	// while after clone and pull, chmod and chown will be executed in
	// order to alter the file mode and owner of the files, or contains
	// files that were created or written by users with differed uid
	// and gid, which would cause
	// 	'fatal: detected dubious ownership in repository'
	// error when performing later on git commands.
	// Hence, we should set the safe.directory to * to avoid
	// further errors.
	err := d.configGlobalSetSafeDirectory(logger, finalizedGitDir, "*")
	if err != nil {
		return err
	}

	return nil
}

func (d *GitLoader) syncWithClone(logger *logrus.Entry, fromURI string, alteredFromURI string, toPath string, finalizedGitDir string) error {
	defer func() {
		if (d.gitOptions.username != "" || d.gitOptions.password != "") || (d.gitOptions.token != "") {
			err := d.remoteSetURL(logger, fromURI, finalizedGitDir, "origin")
			if err != nil {
				logger.Warnf("failed to set remote url for git repository, err: %s", err)
			}
		}
	}()

	err := d.clone(logger, alteredFromURI, toPath)
	if err != nil {
		return err
	}

	err = d.configBeforeOperations(logger, finalizedGitDir)
	if err != nil {
		return err
	}

	// otherwise, after PostCopy stages, the file mode will be changed to d.Options.Mode and
	// result in massive files being changed in the git repository
	err = d.configSetFileMode(logger, finalizedGitDir, false)
	if err != nil {
		return err
	}

	if d.gitOptions.Commit != "" {
		err = d.checkoutCommit(logger, finalizedGitDir)
		if err != nil {
			return err
		}
	}

	return nil
}

func (d *GitLoader) syncWithPull(logger *logrus.Entry, _ string, alteredFromURI string, _ string, finalizedGitDir string) error {
	err := d.configBeforeOperations(logger, finalizedGitDir)
	if err != nil {
		return err
	}

	err = d.updateIndex(logger, finalizedGitDir)
	if err != nil {
		// update index is ignorable
		logger.Warnf("failed to update index for git repository, err: %s", err)
	}

	err = d.addAll(logger, finalizedGitDir)
	if err != nil {
		return err
	}

	err = d.stashPendingChanges(logger, finalizedGitDir)
	if err != nil {
		return err
	}

	err = d.resetHardToTargetBranch(logger, finalizedGitDir, d.gitOptions.Branch)
	if err != nil {
		return err
	}

	pullRemoteName := fmt.Sprintf("dataset-pull-remote-%s", utils.RandomHashString(8))

	defer func() {
		if (d.gitOptions.username != "" || d.gitOptions.password != "") || (d.gitOptions.token != "") {
			err := d.remoteRemove(logger, finalizedGitDir, pullRemoteName)
			if err != nil {
				logger.Warnf("failed to remove remote for git repository, err: %s", err)
			}
		}
	}()

	err = d.remoteAddURL(logger, alteredFromURI, finalizedGitDir, pullRemoteName)
	if err != nil {
		return err
	}

	err = d.pull(logger, alteredFromURI, finalizedGitDir, pullRemoteName)
	if err != nil {
		return err
	}

	if d.gitOptions.Commit != "" {
		err = d.checkoutCommit(logger, finalizedGitDir)
		if err != nil {
			return err
		}
	}

	return nil
}

func (d *GitLoader) Sync(fromURI string, toPath string) error {
	var err error

	alteredFromURI := fromURI
	if d.gitOptions.token != "" {
		alteredFromURI = d.alterFromURIForToken(fromURI, d.gitOptions.token)
	}
	if d.gitOptions.username != "" {
		alteredFromURI = d.alterFromURIForUsernameAndPasswordAccess(fromURI, d.gitOptions.username, d.gitOptions.password)
	}
	if d.gitOptions.sshPrivateKey != "" {
		d.gitOptions.sshPrivateKeyFullPath, err = preparePrivateKeyToSSHDir(d.gitOptions.sshPrivateKey, d.gitOptions.sshPrivateKeyPassphrase)
		if err != nil {
			return err
		}
	}

	logger := log.WithFields(logrus.Fields{
		"fromURI":                     utils.ObscureString(fromURI, d.secrets()),
		"alteredFromURI":              utils.ObscureString(alteredFromURI, d.secrets()),
		"type":                        TypeGit,
		"branch":                      d.gitOptions.Branch,
		"commit":                      d.gitOptions.Commit,
		"depth":                       d.gitOptions.Depth,
		"submodules":                  d.gitOptions.Submodules,
		"applicationWorkingDirectory": lo.Must(os.Getwd()),
		"root":                        d.Options.Root,
		"path":                        toPath,
	})

	finalizedGitDir := filepath.Join(d.Options.Root, toPath)

	checkingGitDir := filepath.Join(finalizedGitDir, ".git")
	stats, err := os.Stat(checkingGitDir)
	if err != nil {
		if !os.IsNotExist(err) {
			return fmt.Errorf("failed to stat %s before pull or clone for git repository, err: %s", checkingGitDir, err)
		}

		return d.syncWithClone(logger, fromURI, alteredFromURI, toPath, finalizedGitDir)
	}
	if !stats.IsDir() {
		return fmt.Errorf("failed to pull or clone for git repository, %s is not a directory", checkingGitDir)
	}

	return d.syncWithPull(logger, fromURI, alteredFromURI, toPath, finalizedGitDir)
}
