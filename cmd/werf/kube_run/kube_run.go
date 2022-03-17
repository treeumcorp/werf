package kube_run

import (
	"context"
	"fmt"
	"math/rand"
	"os"
	"os/exec"
	"strings"

	"github.com/docker/cli/cli"
	"github.com/spf13/cobra"
	"github.com/werf/kubedog/pkg/kube"
	"github.com/werf/logboek"
	"github.com/werf/werf/cmd/werf/common"
	"github.com/werf/werf/pkg/build"
	"github.com/werf/werf/pkg/config"
	"github.com/werf/werf/pkg/config/deploy_params"
	"github.com/werf/werf/pkg/container_runtime"
	"github.com/werf/werf/pkg/git_repo"
	"github.com/werf/werf/pkg/git_repo/gitdata"
	"github.com/werf/werf/pkg/giterminism_manager"
	"github.com/werf/werf/pkg/image"
	"github.com/werf/werf/pkg/logging"
	"github.com/werf/werf/pkg/ssh_agent"
	"github.com/werf/werf/pkg/storage/lrumeta"
	"github.com/werf/werf/pkg/storage/manager"
	"github.com/werf/werf/pkg/tmp_manager"
	"github.com/werf/werf/pkg/true_git"
	"github.com/werf/werf/pkg/werf"
	"github.com/werf/werf/pkg/werf/global_warnings"
	corev1 "k8s.io/api/core/v1"
	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/fields"
	"k8s.io/apimachinery/pkg/util/validation"
)

type cmdDataType struct {
	Interactive     bool
	AllocateTty     bool
	Rm              bool
	RmWithNamespace bool

	Pod          string
	Command      []string
	ImageName    string
	Overrides    string
	ExtraOptions string
}

var (
	cmdData       cmdDataType
	commonCmdData common.CmdData
)

func NewCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:                   "kube-run [options] [IMAGE_NAME] [-- COMMAND ARG...]",
		Short:                 "Run container for project image in Kubernetes",
		Long:                  common.GetLongCommandDescription(`Run container in Kubernetes for specified project image from werf.yaml (build if needed)`),
		DisableFlagsInUseLine: true,
		Example: `  # Run specified image
  $ werf kube-run --repo test/test application

  # Run interactive shell in the image
  $ werf kube-run --repo test/test -it -- sh

  # Run image with specified command
  $ werf kube-run --repo test/test -- /app/run.sh`,
		Annotations: map[string]string{
			common.DisableOptionsInUseLineAnno: "1",
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := common.GetContext()

			defer global_warnings.PrintGlobalWarnings(ctx)

			if err := common.ProcessLogOptions(&commonCmdData); err != nil {
				common.PrintHelp(cmd)
				return err
			}

			if err := processArgs(cmd, args); err != nil {
				common.PrintHelp(cmd)
				return err
			}

			if cmdData.RmWithNamespace && !cmdData.Rm {
				return fmt.Errorf("option --rm-with-namespace requires --rm to be set")
			}

			if cmdData.AllocateTty && !cmdData.Interactive {
				return fmt.Errorf("option --tty requires --interactive to be set")
			}

			if cmdData.Pod != "" {
				if errMsgs := validation.IsDNS1123Subdomain(cmdData.Pod); len(errMsgs) > 0 {
					return fmt.Errorf("--pod name is not a valid subdomain:\n%s", strings.Join(errMsgs, "\n"))
				}
			}

			return runMain(ctx)
		},
	}

	common.SetupDir(&commonCmdData, cmd)
	common.SetupGitWorkTree(&commonCmdData, cmd)
	common.SetupConfigTemplatesDir(&commonCmdData, cmd)
	common.SetupConfigPath(&commonCmdData, cmd)
	common.SetupEnvironment(&commonCmdData, cmd)
	common.SetupNamespace(&commonCmdData, cmd)

	common.SetupGiterminismOptions(&commonCmdData, cmd)

	common.SetupTmpDir(&commonCmdData, cmd)
	common.SetupHomeDir(&commonCmdData, cmd)
	common.SetupSSHKey(&commonCmdData, cmd)

	common.SetupSecondaryStagesStorageOptions(&commonCmdData, cmd)
	common.SetupCacheStagesStorageOptions(&commonCmdData, cmd)
	common.SetupStagesStorageOptions(&commonCmdData, cmd)
	common.SetupFinalStagesStorageOptions(&commonCmdData, cmd)

	common.SetupSkipBuild(&commonCmdData, cmd)

	common.SetupFollow(&commonCmdData, cmd)

	common.SetupDockerConfig(&commonCmdData, cmd, "Command needs granted permissions to read and pull images from the specified repo")
	common.SetupInsecureRegistry(&commonCmdData, cmd)
	common.SetupInsecureHelmDependencies(&commonCmdData, cmd)
	common.SetupSkipTlsVerifyRegistry(&commonCmdData, cmd)

	common.SetupLogOptions(&commonCmdData, cmd)
	common.SetupLogProjectDir(&commonCmdData, cmd)

	common.SetupSynchronization(&commonCmdData, cmd)
	common.SetupKubeConfig(&commonCmdData, cmd)
	common.SetupKubeConfigBase64(&commonCmdData, cmd)
	common.SetupKubeContext(&commonCmdData, cmd)

	common.SetupVirtualMerge(&commonCmdData, cmd)

	common.SetupPlatform(&commonCmdData, cmd)

	// FIXME(ilya-lesikov):
	cmd.Flags().StringVarP(&cmdData.Pod, "pod", "", os.Getenv("WERF_POD"), "FIXME (default $WERF_POD)")
	cmd.Flags().StringVarP(&cmdData.Overrides, "overrides", "", os.Getenv("WERF_OVERRIDES"), "FIXME (default $WERF_OVERRIDES)")
	cmd.Flags().StringVarP(&cmdData.ExtraOptions, "extra-options", "", os.Getenv("WERF_EXTRA_OPTIONS"), "FIXME (default $WERF_EXTRA_OPTIONS)")
	cmd.Flags().BoolVarP(&cmdData.Rm, "rm", "", common.GetBoolEnvironmentDefaultTrue("WERF_RM"), "FIXME (default $WERF_RM)")
	cmd.Flags().BoolVarP(&cmdData.RmWithNamespace, "rm-with-namespace", "", common.GetBoolEnvironmentDefaultFalse("WERF_RM_WITH_NAMESPACE"), "FIXME (default $WERF_RM_WITH_NAMESPACE)")
	cmd.Flags().BoolVarP(&cmdData.Interactive, "interactive", "i", common.GetBoolEnvironmentDefaultFalse("WERF_INTERACTIVE"), "FIXME (default $WERF_INTERACTIVE)")
	cmd.Flags().BoolVarP(&cmdData.AllocateTty, "tty", "t", common.GetBoolEnvironmentDefaultFalse("WERF_TTY"), "FIXME (default $WERF_TTY)")

	return cmd
}

func processArgs(cmd *cobra.Command, args []string) error {
	doubleDashInd := cmd.ArgsLenAtDash()
	doubleDashExist := cmd.ArgsLenAtDash() != -1

	if doubleDashExist {
		if doubleDashInd == len(args) {
			return fmt.Errorf("unsupported position args format")
		}

		switch doubleDashInd {
		case 0:
			cmdData.Command = args[doubleDashInd:]
		case 1:
			cmdData.ImageName = args[0]
			cmdData.Command = args[doubleDashInd:]
		default:
			return fmt.Errorf("unsupported position args format")
		}
	} else {
		switch len(args) {
		case 0:
		case 1:
			cmdData.ImageName = args[0]
		default:
			return fmt.Errorf("unsupported position args format")
		}
	}

	return nil
}

func runMain(ctx context.Context) error {
	global_warnings.PostponeMultiwerfNotUpToDateWarning()

	if err := werf.Init(*commonCmdData.TmpDir, *commonCmdData.HomeDir); err != nil {
		return fmt.Errorf("initialization error: %s", err)
	}

	containerRuntime, processCtx, err := common.InitProcessContainerRuntime(ctx, &commonCmdData)
	if err != nil {
		return err
	}
	ctx = processCtx

	gitDataManager, err := gitdata.GetHostGitDataManager(ctx)
	if err != nil {
		return fmt.Errorf("error getting host git data manager: %s", err)
	}

	if err := git_repo.Init(gitDataManager); err != nil {
		return err
	}

	if err := image.Init(); err != nil {
		return err
	}

	if err := lrumeta.Init(); err != nil {
		return err
	}

	if err := true_git.Init(ctx, true_git.Options{LiveGitOutput: *commonCmdData.LogVerbose || *commonCmdData.LogDebug}); err != nil {
		return err
	}

	if err := common.DockerRegistryInit(ctx, &commonCmdData); err != nil {
		return err
	}

	giterminismManager, err := common.GetGiterminismManager(ctx, &commonCmdData)
	if err != nil {
		return err
	}

	common.ProcessLogProjectDir(&commonCmdData, giterminismManager.ProjectDir())

	if err := ssh_agent.Init(ctx, common.GetSSHKey(&commonCmdData)); err != nil {
		return fmt.Errorf("cannot initialize ssh agent: %s", err)
	}
	defer func() {
		if err := ssh_agent.Terminate(); err != nil {
			logboek.Context(ctx).Warn().LogF("WARNING: ssh agent termination failed: %w\n", err)
		}
	}()

	var pod string
	if cmdData.Pod == "" {
		pod = fmt.Sprintf("werf-run-%d", rand.Int())
	} else {
		pod = cmdData.Pod
	}

	_, werfConfig, err := common.GetRequiredWerfConfig(ctx, &commonCmdData, giterminismManager, common.GetWerfConfigOptions(&commonCmdData, false))
	if err != nil {
		return fmt.Errorf("unable to load werf config: %s", err)
	}

	common.SetupOndemandKubeInitializer(*commonCmdData.KubeContext, *commonCmdData.KubeConfig, *commonCmdData.KubeConfigBase64, *commonCmdData.KubeConfigPathMergeList)
	if err := common.GetOndemandKubeInitializer().Init(ctx); err != nil {
		return err
	}

	namespace, err := deploy_params.GetKubernetesNamespace(*commonCmdData.Namespace, *commonCmdData.Environment, werfConfig)
	if err != nil {
		return err
	}

	defer func() {
		cleanupResources(ctx, pod, namespace)
	}()

	// FIXME(ilya-lesikov):
	if *commonCmdData.Follow {
		// if cmdData.Shell || cmdData.Bash {
		// 	return fmt.Errorf("follow mode does not work with --shell and --bash options")
		// }
		//
		// if err := checkDetachDockerOption(); err != nil {
		// 	return err
		// }
		//
		// containerName := getContainerName()
		// if containerName == "" {
		// 	return fmt.Errorf("follow mode does not work without specific container name: pass --name=CONTAINER_NAME with --docker-options option")
		// }
		//
		// return common.FollowGitHead(ctx, &commonCmdData, func(ctx context.Context, headCommitGiterminismManager giterminism_manager.Interface) error {
		// 	if err := safeDockerCliRmFunc(ctx, containerName); err != nil {
		// 		return err
		// 	}
		//
		// 	if err := run(ctx, containerRuntime, headCommitGiterminismManager); err != nil {
		// 		return err
		// 	}
		//
		// 	go func() {
		// 		time.Sleep(500 * time.Millisecond)
		// 		fmt.Printf("Attaching to container %s ...\n", containerName)
		//
		// 		resp, err := docker.ContainerAttach(ctx, containerName, types.ContainerAttachOptions{
		// 			Stream: true,
		// 			Stdout: true,
		// 			Stderr: true,
		// 			Logs:   true,
		// 		})
		// 		if err != nil {
		// 			_, _ = fmt.Fprintln(os.Stderr, "WARNING:", err)
		// 		}
		//
		// 		if _, err := stdcopy.StdCopy(os.Stdout, os.Stderr, resp.Reader); err != nil {
		// 			_, _ = fmt.Fprintln(os.Stderr, "WARNING:", err)
		// 		}
		// 	}()
		//
		// 	return nil
		// })
		return nil
	} else {
		if err := run(ctx, pod, namespace, werfConfig, containerRuntime, giterminismManager); err != nil {
			if statusErr, ok := err.(cli.StatusError); ok {
				common.TerminateWithError(err.Error(), statusErr.StatusCode)
			}

			return err
		}

		return nil
	}
}

func run(ctx context.Context, pod string, namespace string, werfConfig *config.WerfConfig, containerRuntime container_runtime.ContainerRuntime, giterminismManager giterminism_manager.Interface) error {
	projectName := werfConfig.Meta.Project

	projectTmpDir, err := tmp_manager.CreateProjectDir(ctx)
	if err != nil {
		return fmt.Errorf("getting project tmp dir failed: %s", err)
	}
	defer tmp_manager.ReleaseProjectDir(projectTmpDir)

	imageName := cmdData.ImageName
	if imageName == "" && len(werfConfig.GetAllImages()) == 1 {
		imageName = werfConfig.GetAllImages()[0].GetName()
	}

	if !werfConfig.HasImage(imageName) {
		return fmt.Errorf("image %q is not defined in werf.yaml", logging.ImageLogName(imageName, false))
	}

	stagesStorageAddress, err := common.GetStagesStorageAddress(&commonCmdData)
	if err != nil {
		return err
	}
	stagesStorage, err := common.GetStagesStorage(stagesStorageAddress, containerRuntime, &commonCmdData)
	if err != nil {
		return err
	}
	finalStagesStorage, err := common.GetOptionalFinalStagesStorage(containerRuntime, &commonCmdData)
	if err != nil {
		return err
	}
	synchronization, err := common.GetSynchronization(ctx, &commonCmdData, projectName, stagesStorage)
	if err != nil {
		return err
	}
	stagesStorageCache, err := common.GetStagesStorageCache(synchronization)
	if err != nil {
		return err
	}
	storageLockManager, err := common.GetStorageLockManager(ctx, synchronization)
	if err != nil {
		return err
	}
	secondaryStagesStorageList, err := common.GetSecondaryStagesStorageList(stagesStorage, containerRuntime, &commonCmdData)
	if err != nil {
		return err
	}
	cacheStagesStorageList, err := common.GetCacheStagesStorageList(containerRuntime, &commonCmdData)
	if err != nil {
		return err
	}

	storageManager := manager.NewStorageManager(projectName, stagesStorage, finalStagesStorage, secondaryStagesStorageList, cacheStagesStorageList, storageLockManager, stagesStorageCache)

	logboek.Context(ctx).Info().LogOptionalLn()

	conveyorWithRetry := build.NewConveyorWithRetryWrapper(werfConfig, giterminismManager, []string{imageName}, giterminismManager.ProjectDir(), projectTmpDir, ssh_agent.SSHAuthSock, containerRuntime, storageManager, storageLockManager, common.GetConveyorOptions(&commonCmdData))
	defer conveyorWithRetry.Terminate()

	var image string
	if err := conveyorWithRetry.WithRetryBlock(ctx, func(c *build.Conveyor) error {
		if *commonCmdData.SkipBuild {
			if err := c.ShouldBeBuilt(ctx, build.ShouldBeBuiltOptions{}); err != nil {
				return err
			}
		} else {
			if err := c.Build(ctx, build.BuildOptions{}); err != nil {
				return err
			}
		}

		if err := c.FetchLastImageStage(ctx, imageName); err != nil {
			return err
		}

		image = c.GetImageNameForLastImageStage(imageName)
		return nil
	}); err != nil {
		return err
	}

	args := []string{
		"kubectl",
		"run",
		"--namespace", namespace,
		pod,
		"--image", image,
		"--command",
		"--restart", "Never",
		"--quiet",
		"--attach",
	}

	if cmdData.Interactive {
		args = append(args, "-i")
	}

	if cmdData.AllocateTty {
		args = append(args, "-t")
	}

	if cmdData.Overrides != "" {
		args = append(args, "--overrides", cmdData.Overrides)
	}

	if cmdData.ExtraOptions != "" {
		args = append(args, strings.Fields(cmdData.ExtraOptions)...)
	}

	if len(cmdData.Command) > 0 {
		args = append(args, "--")
		args = append(args, cmdData.Command...)
	}

	if err := createNamespace(ctx, namespace); err != nil {
		return fmt.Errorf("unable to create namespace: %w", err)
	}

	return logboek.Streams().DoErrorWithoutProxyStreamDataFormatting(func() error {
		return common.WithoutTerminationSignalsTrap(func() error {
			logboek.Context(ctx).LogF("Running pod %q in namespace %q ...\n", pod, namespace)

			cmd := exec.Command(os.Args[0], args...)
			cmd.Stdout = os.Stdout
			cmd.Stderr = os.Stderr
			cmd.Stdin = os.Stdin

			if err := cmd.Run(); err != nil {
				return fmt.Errorf("error running pod: %w", err)
			}

			return nil
		})
	})
}

func cleanupResources(ctx context.Context, pod string, namespace string) {
	if isNsExist, err := isNamespaceExist(ctx, namespace); err != nil {
		logboek.Context(ctx).Warn().LogF("WARNING: unable to check for namespace existence: %w\n", err)
		return
	} else if !isNsExist {
		return
	}

	if cmdData.Rm {
		if isPodExist, err := isPodExist(ctx, pod, namespace); err != nil {
			logboek.Context(ctx).Warn().LogF("WARNING: unable to check for pod existence: %w\n", err)
		} else if isPodExist {
			logboek.Context(ctx).LogF("Cleaning up pod %q ...\n", pod)
			if err := kube.Client.CoreV1().Pods(namespace).Delete(ctx, pod, v1.DeleteOptions{}); err != nil {
				logboek.Context(ctx).Warn().LogF("WARNING: pod cleaning up failed: %w\n", err)
			}
		}
	}

	if cmdData.RmWithNamespace {
		logboek.Context(ctx).LogF("Cleaning up namespace %q ...\n", namespace)
		if err := kube.Client.CoreV1().Namespaces().Delete(ctx, namespace, v1.DeleteOptions{}); err != nil {
			logboek.Context(ctx).Warn().LogF("WARNING: namespace cleaning up failed: %w\n", err)
		}
	}
}

func createNamespace(ctx context.Context, namespace string) error {
	if isNsExist, err := isNamespaceExist(ctx, namespace); err != nil {
		return fmt.Errorf("unable to check for namespace existence: %w", err)
	} else if isNsExist {
		return nil
	}

	logboek.Context(ctx).LogF("Creating namespace %q ...\n", namespace)

	kube.Client.CoreV1().Namespaces().Create(
		ctx,
		&corev1.Namespace{
			ObjectMeta: v1.ObjectMeta{
				Name: namespace,
			},
		},
		v1.CreateOptions{},
	)

	return nil
}

func isNamespaceExist(ctx context.Context, namespace string) (bool, error) {
	if matchedNamespaces, err := kube.Client.CoreV1().Namespaces().List(ctx, v1.ListOptions{
		FieldSelector: fields.OneTermEqualSelector("metadata.name", namespace).String(),
	}); err != nil {
		return false, fmt.Errorf("unable to list namespaces: %w", err)
	} else if len(matchedNamespaces.Items) > 0 {
		return true, nil
	}

	return false, nil
}

func isPodExist(ctx context.Context, pod string, namespace string) (bool, error) {
	if matchedPods, err := kube.Client.CoreV1().Pods(namespace).List(ctx, v1.ListOptions{
		FieldSelector: fields.OneTermEqualSelector("metadata.name", pod).String(),
	}); err != nil {
		return false, fmt.Errorf("unable to list pods: %w", err)
	} else if len(matchedPods.Items) > 0 {
		return true, nil
	}

	return false, nil
}
