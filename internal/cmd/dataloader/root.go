package dataloader

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"

	"github.com/samber/lo"
	"github.com/spf13/cobra"

	"github.com/BaizeAI/dataset/internal/pkg/constants"
	"github.com/BaizeAI/dataset/internal/pkg/datasources"
	"github.com/BaizeAI/dataset/pkg/log"
	"github.com/BaizeAI/dataset/pkg/utils"
)

func NewCommand() *cobra.Command {
	supportedTypesOfDataSourcesCommandHelpStr := strings.Join(datasources.SupportedTypesString, "|")

	rootCmd := &cobra.Command{
		Use:   fmt.Sprintf("data-loader [%s] <uri>", supportedTypesOfDataSourcesCommandHelpStr),
		Short: "Load datasets from various data sources",
	}

	flags := new(CommandFlags)

	rootCmd.Flags().StringVar(&flags.MountPath, "mount-path", "", "Mount path for data source to copy to")
	rootCmd.Flags().StringVar(&flags.MountMode, "mount-mode", "0755", "Mount mode for data source to copy to")
	rootCmd.Flags().IntVar(&flags.MountUID, "mount-uid", 1000, "Mount UID for data source to copy to")
	rootCmd.Flags().IntVar(&flags.MountGID, "mount-gid", 1000, "Mount GID for data source to copy to")
	rootCmd.Flags().StringVar(&flags.MountRoot, "mount-root", "", "Mount root for data source to copy to")
	rootCmd.Flags().StringVar(&flags.MountSecrets, "mount-secrets", constants.DatasetJobSecretsMountPath, "Mount secrets for data source to copy to")
	rootCmd.Flags().StringArrayVarP(&flags.Options, "options", "o", []string{}, "Options for data source to copy from")

	rootCmd.Args = newCommandValidateArgsFunc(flags)
	rootCmd.Run = newCommandRunEFunc(flags)

	return rootCmd
}

var (
	optionsRegexp = regexp.MustCompile(`^(\w+)=(.*)$`)
)

type CommandFlags struct {
	MountPath    string
	MountMode    string
	MountUID     int
	MountGID     int
	MountRoot    string
	MountSecrets string
	Options      []string
}

func newCommandValidateArgsFunc(flags *CommandFlags) func(cmd *cobra.Command, args []string) error {
	return func(cmd *cobra.Command, args []string) error {
		if len(args) < 2 || args[0] == "" || args[1] == "" {
			return fmt.Errorf("arguments <type> and <uri> are required")
		}
		if !lo.Contains(datasources.SupportedTypesString, args[0]) {
			return fmt.Errorf("data source type %s is not supported, supported types are %s", args[0], strings.Join(datasources.SupportedTypesString, ", "))
		}
		if flags.MountPath == "" {
			return fmt.Errorf("flag --mount-path is required")
		}

		return nil
	}
}

func execPostCopy(_ map[string]string, datasourceOptions datasources.Options, _ datasources.Secrets) error {
	err := utils.ChmodAndChownRecursively(
		log.WithField("action", "post copy"),
		filepath.Join(datasourceOptions.Root, datasourceOptions.Path),
		datasourceOptions.UID,
		datasourceOptions.GID,
		datasourceOptions.Mode,
	)
	if err != nil {
		return fmt.Errorf("failed to perform post chmod and chown operations, err: %w", err)
	}

	return nil
}

func execCopy(rawOptions map[string]string, datasourceOptions datasources.Options, secrets datasources.Secrets) error {
	var err error
	var datasourceLoader datasources.Loader

	switch datasourceOptions.Type {
	case datasources.TypeS3:
		datasourceLoader, err = datasources.NewS3Loader(rawOptions, datasourceOptions, secrets)
		if err != nil {
			return err
		}
	case datasources.TypeHTTP:
		datasourceLoader, err = datasources.NewHTTPLoader(rawOptions, datasourceOptions, secrets)
		if err != nil {
			return err
		}
	case datasources.TypeGit:
		datasourceLoader, err = datasources.NewGitLoader(rawOptions, datasourceOptions, secrets)
		if err != nil {
			return err
		}
	case datasources.TypeConda:
		datasourceLoader, err = datasources.NewCondaLoader(rawOptions, datasourceOptions, secrets)
		if err != nil {
			return err
		}
	case datasources.TypeHuggingFace:
		datasourceLoader, err = datasources.NewHuggingFaceLoader(rawOptions, datasourceOptions, secrets)
		if err != nil {
			return err
		}
	case datasources.TypeModelScope:
		datasourceLoader, err = datasources.NewModelScopeLoader(rawOptions, datasourceOptions, secrets)
		if err != nil {
			return err
		}
	case datasources.TypeDatabase:
		datasourceLoader, err = datasources.NewModelDatabaseLoader(rawOptions, datasourceOptions, secrets)
		if err != nil {
			return err
		}
	case datasources.TypeHadoop:
		datasourceLoader, err = datasources.NewModelHadoopLoader(rawOptions, datasourceOptions, secrets)
		if err != nil {
			return err
		}
	default:
		return fmt.Errorf("data source type %s is not supported", datasourceOptions.Type)
	}

	err = datasourceLoader.Sync(datasourceOptions.URI, datasourceOptions.Path)
	if err != nil {
		return err
	}

	return nil
}

func newCommandRunEFunc(flags *CommandFlags) func(cmd *cobra.Command, args []string) {
	return func(cmd *cobra.Command, args []string) {
		flags.MountPath = filepath.Join(".", flags.MountPath)

		if flags.MountRoot == "" {
			flags.MountRoot = lo.Must(os.Getwd())
		}

		options := make(map[string]string)
		for _, optionStr := range flags.Options {
			option := optionsRegexp.FindStringSubmatch(optionStr)

			if len(option) == 3 {
				options[option[1]] = option[2]
			} else {
				options[option[1]] = ""
			}
		}

		fileMode, err := strconv.ParseUint(flags.MountMode, 8, 32)
		if err != nil {
			handleError(err)
			return
		}

		datasourceOptions := datasources.Options{
			Type: datasources.Type(args[0]),
			URI:  args[1],
			Path: flags.MountPath,
			Root: flags.MountRoot,
			UID:  flags.MountUID,
			GID:  flags.MountGID,
			Mode: os.FileMode(fileMode),
		}

		secrets, err := datasources.ReadAndParseSecrets(flags.MountSecrets)
		if err != nil {
			log.Warnf("failed to read and parse secrets from %s, err: %s", constants.DatasetJobSecretsMountPath, err)
		}

		err = execCopy(options, datasourceOptions, secrets)
		if err != nil {
			handleError(err)
		}

		err = execPostCopy(options, datasourceOptions, secrets)
		if err != nil {
			handleError(err)
		}
	}
}

func handleError(err error) {
	if err == nil {
		return
	}

	_, err = fmt.Fprintf(os.Stderr, "failed to load data: %s\n", err)
	if err != nil {
		panic(err)
	}

	os.Exit(1)
}
