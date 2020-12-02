package secret

import (
	"fmt"
	"strings"

	"github.com/spf13/cobra"

	"github.com/werf/werf/cmd/werf/common"
	secret_common "github.com/werf/werf/cmd/werf/helm/secret/common"
	"github.com/werf/werf/pkg/deploy/secret"
	"github.com/werf/werf/pkg/git_repo"
	"github.com/werf/werf/pkg/gitermenism_inspector"
	"github.com/werf/werf/pkg/werf"
)

var CmdData struct {
	OutputFilePath string
}

var commonCmdData common.CmdData

func NewCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:                   "decrypt [FILE_PATH]",
		DisableFlagsInUseLine: true,
		Short:                 "Decrypt secret file data",
		Long: common.GetLongCommandDescription(`Decrypt data from FILE_PATH or pipe.
Encryption key should be in $WERF_SECRET_KEY or .werf_secret_key file`),
		Example: `  # Decrypt secret file
  $ werf helm secret file decrypt .helm/secret/privacy

  # Decrypt from a pipe
  $ cat .helm/secret/date | werf helm secret decrypt
  Tue Jun 26 09:58:10 PDT 1990`,
		Annotations: map[string]string{
			common.CmdEnvAnno: common.EnvsDescription(common.WerfSecretKey),
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := common.ProcessLogOptions(&commonCmdData); err != nil {
				common.PrintHelp(cmd)
				return err
			}

			var filePath string

			if len(args) > 0 {
				filePath = args[0]
			}

			if err := runSecretDecrypt(filePath); err != nil {
				if strings.HasSuffix(err.Error(), secret_common.ExpectedFilePathOrPipeError().Error()) {
					common.PrintHelp(cmd)
				}

				return err
			}

			return nil
		},
	}

	common.SetupDir(&commonCmdData, cmd)
	common.SetupTmpDir(&commonCmdData, cmd)
	common.SetupHomeDir(&commonCmdData, cmd)

	common.SetupDisableGitermenism(&commonCmdData, cmd)
	common.SetupNonStrictGitermenismInspection(&commonCmdData, cmd)

	common.SetupLogOptions(&commonCmdData, cmd)

	cmd.Flags().StringVarP(&CmdData.OutputFilePath, "output-file-path", "o", "", "Write to file instead of stdout")

	return cmd
}

func runSecretDecrypt(filePath string) error {
	if err := werf.Init(*commonCmdData.TmpDir, *commonCmdData.HomeDir); err != nil {
		return fmt.Errorf("initialization error: %s", err)
	}

	if err := gitermenism_inspector.Init(gitermenism_inspector.InspectionOptions{DisableGitermenism: *commonCmdData.DisableGitermenism, NonStrict: *commonCmdData.NonStrictGitermenismInspection}); err != nil {
		return err
	}

	if err := git_repo.Init(); err != nil {
		return err
	}

	projectDir, err := common.GetProjectDir(&commonCmdData)
	if err != nil {
		return fmt.Errorf("getting project dir failed: %s", err)
	}

	m, err := secret.GetManager(projectDir)
	if err != nil {
		return err
	}

	return secret_common.SecretFileDecrypt(m, filePath, CmdData.OutputFilePath)
}
