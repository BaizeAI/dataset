package datasources

import (
	"encoding/base64"
	"fmt"
	"net/url"
	"os"
	"os/exec"
	"strings"

	"github.com/sirupsen/logrus"

	"github.com/BaizeAI/dataset/pkg/log"
	"github.com/BaizeAI/dataset/pkg/utils"
)

var _ Loader = &HTTPLoader{}

type HTTPLoader struct {
	Options Options

	httpOptions HTTPLoaderOptions
}

func NewHTTPLoader(datasourceOptions map[string]string, options Options, secrets Secrets) (*HTTPLoader, error) {
	h := new(HTTPLoader)

	h.Options = options

	_, err := url.Parse(options.URI)
	if err != nil {
		return nil, fmt.Errorf("failed to parse uri %s: %w", options.URI, err)
	}

	h.httpOptions.basicAuthUsername = secrets.Username
	h.httpOptions.basicAuthPassword = secrets.Password

	return h, nil
}

type HTTPLoaderOptions struct {
	basicAuthUsername string
	basicAuthPassword string

	fromURI string
}

func (d *HTTPLoader) configTouch() error {
	return rcloneCliConfigTouch()
}

func (d *HTTPLoader) configCreate(configName string) error {
	logger := log.WithFields(logrus.Fields{
		"configName": configName,
		"type":       TypeHTTP,
		"fromURI":    d.httpOptions.fromURI,
	})

	args := []string{
		"config",
		"create",
		configName,
		"http",
		strings.Join([]string{"url", d.httpOptions.fromURI}, "="),
	}

	cmd := exec.Command("rclone", args...)

	logger = logger.WithField("command", cmd.String())
	logger.Debug("executing command to create a new rclone config")

	outBuffer, errBuffer, err := utils.ExecuteCommandWithAllOutput(logger, cmd, nil)

	if err != nil {
		logger.Errorf("rclone config create command error: %s", errBuffer)
		return err
	}
	logger.Debugf("rclone config create command output: %s", outBuffer.String())

	return nil
}

// From https://cs.opensource.google/go/go/+/refs/tags/go1.21.5:src/net/http/client.go;l=426
func basicAuth(username, password string) string {
	auth := username + ":" + password
	return base64.StdEncoding.EncodeToString([]byte(auth))
}

func (d *HTTPLoader) Sync(fromURI string, toPath string) error {
	_, err := url.Parse(fromURI)
	if err != nil {
		return fmt.Errorf("failed to parse uri %s: %w", fromURI, err)
	}

	logger := log.WithFields(logrus.Fields{
		"fromURI":          fromURI,
		"type":             TypeHTTP,
		"toPath":           toPath,
		"workingDirectory": d.Options.Root,
	})

	basicAuthUsername := strings.TrimSpace(d.httpOptions.basicAuthUsername)
	basicAuthPassword := strings.TrimSpace(d.httpOptions.basicAuthPassword)
	basicAuthBase64 := basicAuth(basicAuthUsername, basicAuthPassword)

	logger.Debugf("performing rclone copy command to copy data served by HTTP")

	err = d.configTouch()
	if err != nil {
		return err
	}

	configName := fmt.Sprintf("baize-data-loader-copy-config-%s", utils.RandomHashString(8))
	d.httpOptions.fromURI = fromURI

	err = d.configCreate(configName)
	if err != nil {
		return err
	}

	args := []string{
		"sync",
		fmt.Sprintf("%s:", configName),
		toPath,
	}

	args = append(args, "-vvv")

	cmd := exec.Command("rclone", args...)
	cmd.Dir = d.Options.Root

	logger = logger.WithField("command", cmd.String())
	logger.Debug("executing command to copy data")

	cmd.Env = os.Environ()

	if basicAuthUsername != "" && basicAuthPassword != "" {
		cmd.Env = append(cmd.Env, fmt.Sprintf("RCLONE_HTTP_HEADERS=Authorization,Basic %s", basicAuthBase64))
	}

	outBuffer, errBuffer, err := utils.ExecuteCommandWithAllOutput(logger, cmd, []string{basicAuthBase64})
	if err != nil {
		logger.Errorf("rclone copy command error: %s", errBuffer)
		return fmt.Errorf("failed to copy data from %s to %s with rclone command %s, err: %s", fromURI, toPath, cmd.String(), err)
	}
	logger.Debugf("rclone copy command output: %s", outBuffer.String())

	return nil
}
