package utils

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"

	"github.com/sirupsen/logrus"
)

func ExecuteCommandWithAllOutput(logger *logrus.Entry, cmd *exec.Cmd, secrets []string) (*bytes.Buffer, *bytes.Buffer, error) {
	logger = logger.WithField("command", ObscureString(cmd.String(), secrets))
	logger.Debug("executing command")

	outBuffer, errBuffer := RedirectCmdWithObscureOutputWriter(cmd, secrets)

	p, err := exec.LookPath(cmd.Path)
	if err != nil {
		return outBuffer, errBuffer, err
	}
	f, err := os.Open(p)
	if err != nil {
		return outBuffer, errBuffer, err
	}
	defer func() {
		_ = f.Close()
	}()
	bs := make([]byte, 4)
	_, err = f.Read(bs)
	if err != nil {
		return outBuffer, errBuffer, err
	}
	if strings.Contains(string(bs), "#") {
		cmd.Args = []string{"sh", "-c", fmt.Sprintf("%s %s", cmd.Path, strings.Join(cmd.Args[1:], " "))}
		shellPath, _ := exec.LookPath("sh")
		cmd.Path = shellPath
	}

	err = cmd.Run()
	logger.Debugf("command output: %s", outBuffer.String())
	if err != nil {
		logger.Errorf("command failed to execute, error: %s", errBuffer.String())
		return outBuffer, errBuffer, fmt.Errorf("failed to execute command %s, err: %s", ObscureString(cmd.String(), secrets), err)
	}

	return outBuffer, errBuffer, nil
}

func ExecuteCommandWithOutput(logger *logrus.Entry, cmd *exec.Cmd, secrets []string) (*bytes.Buffer, error) {
	outBuffer, _, err := ExecuteCommandWithAllOutput(logger, cmd, secrets)

	return outBuffer, err
}

func ExecuteCommand(logger *logrus.Entry, cmd *exec.Cmd, secrets []string) error {
	_, err := ExecuteCommandWithOutput(logger, cmd, secrets)
	return err
}

func NewWrappedOutputWriter(wrappedWriter io.Writer) (*bytes.Buffer, io.Writer) {
	buffer := new(bytes.Buffer)
	return buffer, io.MultiWriter(buffer, wrappedWriter)
}

func NewObscuredOutputWriter(wrappedWriter io.Writer, secrets []string) (*bytes.Buffer, io.Writer) {
	buf, writer := NewWrappedOutputWriter(wrappedWriter)
	return buf, NewObscuredWriter(writer, secrets)
}

func RedirectCmdWithObscureOutputWriter(cmd *exec.Cmd, secrets []string) (*bytes.Buffer, *bytes.Buffer) {
	var outBuffer, errBuffer *bytes.Buffer

	outBuffer, cmd.Stdout = NewObscuredOutputWriter(os.Stdout, secrets)
	errBuffer, cmd.Stderr = NewObscuredOutputWriter(os.Stderr, secrets)

	return outBuffer, errBuffer
}
