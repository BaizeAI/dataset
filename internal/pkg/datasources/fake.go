package datasources

import (
	"bytes"
	"fmt"
	"os"
	"path"
	"strconv"
	"testing"
	"text/template"

	"github.com/stretchr/testify/require"
)

type out struct {
	stdout string
	stderr string
	exit   int
}

type fakeCommand struct {
	t *testing.T

	cmd     string
	path    string
	outputs []out
}

func (f *fakeCommand) Inject() error {
	if f.path == "" {
		f.path, _ = os.MkdirTemp("", "fakeCommand-*")
	}
	require.NoError(f.t, os.MkdirAll(f.path, 0755)) // nolint: gosec
	for i, o := range f.outputs {
		require.NoError(f.t, os.WriteFile(path.Join(f.path, fmt.Sprintf(".%s_output_%d", f.cmd, i)), []byte(o.stdout), 0600))
		require.NoError(f.t, os.WriteFile(path.Join(f.path, fmt.Sprintf(".%s_stderr_%d", f.cmd, i)), []byte(o.stderr), 0600))
		require.NoError(f.t, os.WriteFile(path.Join(f.path, fmt.Sprintf(".%s_exit_%d", f.cmd, i)), []byte(strconv.Itoa(o.exit)), 0600))
	}
	t, err := template.New("fakeCommand").Parse(
		`#!/usr/bin/env bash
index=0
if [ -f "{{.path}}/.{{.cmd}}_index" ]; then
	index=$(cat "{{.path}}/.{{.cmd}}_index")
fi
echo $((index+1)) > "{{.path}}/.{{.cmd}}_index"
echo "$*" > "{{.path}}/.{{.cmd}}_input_$index"
cat "{{.path}}/.{{.cmd}}_output_$index"
cat "{{.path}}/.{{.cmd}}_stderr_$index" 1>&2
exit $(cat "{{.path}}/.{{.cmd}}_exit_$index")
`,
	)
	if err != nil {
		return err
	}
	shell := bytes.NewBuffer(nil)
	err = t.Execute(shell, map[string]interface{}{
		"path": f.path,
		"cmd":  f.cmd,
	})
	if err != nil {
		return err
	}
	return os.WriteFile(path.Join(f.path, f.cmd), shell.Bytes(), 0755) // nolint: gosec
}

func (f *fakeCommand) GetInput(index int) ([]byte, error) {
	return os.ReadFile(path.Join(f.path, fmt.Sprintf(".%s_input_%d", f.cmd, index)))
}

func (f *fakeCommand) GetAllInputs() [][]byte {
	var inputs [][]byte
	for i := 0; ; i++ {
		input, err := f.GetInput(i)
		if err != nil {
			break
		}
		inputs = append(inputs, input)
	}
	return inputs
}

func (f *fakeCommand) WithContext(run func()) {
	require.NoError(f.t, f.Inject())
	p := os.Getenv("PATH")
	require.NoError(f.t, os.Setenv("PATH", fmt.Sprintf("%s:%s", f.path, p)))
	run()
	require.NoError(f.t, os.Setenv("PATH", p))
}

func (f *fakeCommand) Clean() error {
	return os.RemoveAll(f.path)
}
