package cmd

import (
	"fmt"
	"reflect"

	"errors"
	cmdconf "github.com/cloudfoundry/bosh-init/cmd/config"
	boshdir "github.com/cloudfoundry/bosh-init/director"
	boshtpl "github.com/cloudfoundry/bosh-init/director/template"
	boshrel "github.com/cloudfoundry/bosh-init/release"
	boshreldir "github.com/cloudfoundry/bosh-init/releasedir"
	boshssh "github.com/cloudfoundry/bosh-init/ssh"
	boshui "github.com/cloudfoundry/bosh-init/ui"
	boshuit "github.com/cloudfoundry/bosh-init/ui/task"
	goflags "github.com/jessevdk/go-flags"
	"strings"
)

type Factory struct {
	opts *BoshOpts
}

func NewFactory(deps BasicDeps) Factory {
	var opts BoshOpts

	goflags.FactoryFunc = func(val interface{}) {
		stype := reflect.Indirect(reflect.ValueOf(val))
		if stype.Kind() == reflect.Struct {
			field := stype.FieldByName("FS")
			if field.IsValid() {
				field.Set(reflect.ValueOf(deps.FS))
			}
		}
	}

	globalOptsFuncCalled := false

	rejectExtraArgsFunc := func(nextFunc func() error) func(extraArgs []string) error {
		return func(extraArgs []string) error {
			if len(extraArgs) > 0 {
				return errors.New(fmt.Sprintf("Extra arguments are not supported for this command: %s",
					strings.Join(extraArgs, ", ")))
			}

			return nextFunc()
		}
	}

	rejectExtraArgsWithDirectoryFunc := func(nextFunc func(DirOrCWDArg) error) func(extraArgs []string, dir DirOrCWDArg) error {
		return func(extraArgs []string, dir DirOrCWDArg) error {
			rejectExtraArgs := rejectExtraArgsFunc(func() error {
				return nil
			})

			err := rejectExtraArgs(extraArgs)
			if err != nil {
				return err
			}

			return nextFunc(dir)
		}
	}

	globalOptsFunc := func(nextFunc func() error) func() error {
		return func() error {
			if globalOptsFuncCalled {
				return nextFunc()
			}

			globalOptsFuncCalled = true

			deps.UI.EnableTTY(opts.TTYOpt)

			deps.UI.EnableColor()

			if opts.JSONOpt {
				deps.UI.EnableJSON()
			}

			if opts.NonInteractiveOpt {
				deps.UI.EnableNonInteractive()
			}

			tmpDirPath, err := deps.FS.ExpandPath("~/.bosh/tmp")
			if err != nil {
				return err
			}

			err = deps.FS.ChangeTempRoot(tmpDirPath)
			if err != nil {
				return err
			}

			return nextFunc()
		}
	}

	configFunc := func(nextFunc func(cmdconf.Config) error) func() error {
		return globalOptsFunc(func() error {
			config, err := cmdconf.NewFSConfigFromPath(opts.ConfigPathOpt, deps.FS)
			if err != nil {
				return err
			}

			return nextFunc(config)
		})
	}

	sessFunc := func(nextFunc func(Session) error) func() error {
		return configFunc(func(config cmdconf.Config) error {
			sess := NewSessionFromOpts(opts, config, deps.UI, true, true, deps.FS, deps.Logger)
			return nextFunc(sess)
		})
	}

	directorFunc := func(nextFunc func(boshdir.Director) error) func() error {
		return sessFunc(func(sess Session) error {
			director, err := sess.Director()
			if err != nil {
				return err
			}

			return nextFunc(director)
		})
	}

	deploymentFunc := func(nextFunc func(boshdir.Deployment) error) func() error {
		return sessFunc(func(sess Session) error {
			deployment, err := sess.Deployment()
			if err != nil {
				return err
			}

			return nextFunc(deployment)
		})
	}

	directorAndDeploymentFunc := func(nextFunc func(boshdir.Director, boshdir.Deployment) error) func() error {
		return sessFunc(func(sess Session) error {
			director, err := sess.Director()
			if err != nil {
				return err
			}

			deployment, err := sess.Deployment()
			if err != nil {
				return err
			}

			return nextFunc(director, deployment)
		})
	}

	releaseProvidersFunc := func(nextFunc func(boshrel.Provider, boshreldir.Provider) error) func() error {
		return globalOptsFunc(func() error {
			indexReporter := boshui.NewIndexReporter(deps.UI)
			blobsReporter := boshui.NewBlobsReporter(deps.UI)
			releaseIndexReporter := boshui.NewReleaseIndexReporter(deps.UI)

			releaseProvider := boshrel.NewProvider(
				deps.CmdRunner, deps.Compressor, deps.SHA1Calc, deps.FS, deps.Logger)

			releaseDirProvider := boshreldir.NewProvider(
				indexReporter, releaseIndexReporter, blobsReporter, releaseProvider,
				deps.SHA1Calc, deps.CmdRunner, deps.UUIDGen, deps.FS, deps.Logger)

			return nextFunc(releaseProvider, releaseDirProvider)
		})
	}

	blobsDirFunc := func(nextFunc func(boshreldir.BlobsDir) error) func(DirOrCWDArg) error {
		return func(dir DirOrCWDArg) error {
			return releaseProvidersFunc(func(_ boshrel.Provider, relDirProv boshreldir.Provider) error {
				return nextFunc(relDirProv.NewFSBlobsDir(dir.Path))
			})()
		}
	}

	releaseDirFunc := func(nextFunc func(boshreldir.ReleaseDir) error) func(DirOrCWDArg) error {
		return func(dir DirOrCWDArg) error {
			return releaseProvidersFunc(func(_ boshrel.Provider, relDirProv boshreldir.Provider) error {
				return nextFunc(relDirProv.NewFSReleaseDir(dir.Path))
			})()
		}
	}

	opts.VersionOpt = func() error {
		return &goflags.Error{
			Type:    goflags.ErrHelp,
			Message: fmt.Sprintf("version %s", VersionLabel),
		}
	}

	opts.CreateEnv.call = rejectExtraArgsFunc(globalOptsFunc(func() error {
		envProvider := func(path string, vars boshtpl.Variables) DeploymentPreparer {
			return NewEnvFactory(deps, path, vars).Preparer()
		}

		stage := boshui.NewStage(deps.UI, deps.Time, deps.Logger)
		return NewDeployCmd(deps.UI, envProvider).Run(stage, opts.CreateEnv)
	}))

	opts.DeleteEnv.call = rejectExtraArgsFunc(globalOptsFunc(func() error {
		envProvider := func(path string, vars boshtpl.Variables) DeploymentDeleter {
			return NewEnvFactory(deps, path, vars).Deleter()
		}

		stage := boshui.NewStage(deps.UI, deps.Time, deps.Logger)
		return NewDeleteCmd(deps.UI, envProvider).Run(stage, opts.DeleteEnv)
	}))

	opts.Environments.call = rejectExtraArgsFunc(configFunc(func(config cmdconf.Config) error {
		return NewEnvironmentsCmd(config, deps.UI).Run()
	}))

	opts.Environment.call = rejectExtraArgsFunc(configFunc(func(config cmdconf.Config) error {
		sessionFactory := func(config cmdconf.Config) Session {
			return NewSessionFromOpts(opts, config, deps.UI, false, false, deps.FS, deps.Logger)
		}

		return NewEnvironmentCmd(sessionFactory, config, deps.UI).Run(opts.Environment)
	}))

	opts.LogIn.call = rejectExtraArgsFunc(configFunc(func(config cmdconf.Config) error {
		sessionFactory := func(config cmdconf.Config) Session {
			return NewSessionFromOpts(opts, config, deps.UI, true, true, deps.FS, deps.Logger)
		}

		basicStrategy := NewBasicLoginStrategy(sessionFactory, config, deps.UI)
		uaaStrategy := NewUAALoginStrategy(sessionFactory, config, deps.UI, deps.Logger)

		sess := NewSessionFromOpts(opts, config, deps.UI, true, true, deps.FS, deps.Logger)

		anonDirector, err := sess.AnonymousDirector()
		if err != nil {
			return err
		}

		return NewLogInCmd(basicStrategy, uaaStrategy, anonDirector).Run()
	}))

	opts.LogOut.call = rejectExtraArgsFunc(configFunc(func(config cmdconf.Config) error {
		sess := NewSessionFromOpts(opts, config, deps.UI, true, true, deps.FS, deps.Logger)
		return NewLogOutCmd(sess.Environment(), config, deps.UI).Run()
	}))

	opts.Task.call = rejectExtraArgsFunc(directorFunc(func(director boshdir.Director) error {
		eventsTaskReporter := boshuit.NewReporter(deps.UI, true)
		plainTaskReporter := boshuit.NewReporter(deps.UI, false)
		return NewTaskCmd(eventsTaskReporter, plainTaskReporter, director).Run(opts.Task)
	}))

	opts.Tasks.call = rejectExtraArgsFunc(directorFunc(func(director boshdir.Director) error {
		return NewTasksCmd(deps.UI, director).Run(opts.Tasks)
	}))

	opts.CancelTask.call = rejectExtraArgsFunc(directorFunc(func(director boshdir.Director) error {
		return NewCancelTaskCmd(director).Run(opts.CancelTask)
	}))

	opts.Deployment.call = rejectExtraArgsFunc(configFunc(func(config cmdconf.Config) error {
		sessionFactory := func(config cmdconf.Config) Session {
			return NewSessionFromOpts(opts, config, deps.UI, true, false, deps.FS, deps.Logger)
		}

		return NewDeploymentCmd(sessionFactory, config, deps.UI).Run(opts.Deployment)
	}))

	opts.Deployments.call = rejectExtraArgsFunc(directorFunc(func(director boshdir.Director) error {
		return NewDeploymentsCmd(deps.UI, director).Run()
	}))

	opts.DeleteDeployment.call = rejectExtraArgsFunc(deploymentFunc(func(dep boshdir.Deployment) error {
		return NewDeleteDeploymentCmd(deps.UI, dep).Run(opts.DeleteDeployment)
	}))

	opts.Releases.call = rejectExtraArgsFunc(directorFunc(func(director boshdir.Director) error {
		return NewReleasesCmd(deps.UI, director).Run()
	}))

	opts.UploadRelease.call = rejectExtraArgsWithDirectoryFunc(func(dir DirOrCWDArg) error {
		return releaseProvidersFunc(func(relProv boshrel.Provider, relDirProv boshreldir.Provider) error {
			return directorFunc(func(director boshdir.Director) error {
				releaseReader := relDirProv.NewReleaseReader(dir.Path)
				releaseWriter := relProv.NewArchiveWriter()
				releaseDir := relDirProv.NewFSReleaseDir(dir.Path)

				releaseArchiveFactory := func(path string) boshdir.ReleaseArchive {
					return boshdir.NewFSReleaseArchive(path, deps.FS)
				}

				cmd := NewUploadReleaseCmd(
					releaseReader, releaseWriter, releaseDir, director, releaseArchiveFactory, deps.UI)

				return cmd.Run(opts.UploadRelease)
			})()
		})()
	})

	opts.DeleteRelease.call = rejectExtraArgsFunc(directorFunc(func(director boshdir.Director) error {
		return NewDeleteReleaseCmd(deps.UI, director).Run(opts.DeleteRelease)
	}))

	opts.Stemcells.call = rejectExtraArgsFunc(directorFunc(func(director boshdir.Director) error {
		return NewStemcellsCmd(deps.UI, director).Run()
	}))

	opts.UploadStemcell.call = rejectExtraArgsFunc(directorFunc(func(director boshdir.Director) error {
		stemcellArchiveFactory := func(path string) boshdir.StemcellArchive {
			return boshdir.NewFSStemcellArchive(path, deps.FS)
		}

		return NewUploadStemcellCmd(director, stemcellArchiveFactory, deps.UI).Run(opts.UploadStemcell)
	}))

	opts.DeleteStemcell.call = rejectExtraArgsFunc(directorFunc(func(director boshdir.Director) error {
		return NewDeleteStemcellCmd(deps.UI, director).Run(opts.DeleteStemcell)
	}))

	opts.Locks.call = rejectExtraArgsFunc(directorFunc(func(director boshdir.Director) error {
		return NewLocksCmd(deps.UI, director).Run()
	}))

	opts.Errands.call = rejectExtraArgsFunc(deploymentFunc(func(dep boshdir.Deployment) error {
		return NewErrandsCmd(deps.UI, dep).Run()
	}))

	opts.RunErrand.call = rejectExtraArgsFunc(directorAndDeploymentFunc(func(director boshdir.Director, dep boshdir.Deployment) error {
		downloader := NewUIDownloader(director, deps.SHA1Calc, deps.Time, deps.FS, deps.UI)
		return NewRunErrandCmd(dep, downloader, deps.UI).Run(opts.RunErrand)
	}))

	opts.Disks.call = rejectExtraArgsFunc(directorFunc(func(director boshdir.Director) error {
		return NewDisksCmd(deps.UI, director).Run(opts.Disks)
	}))

	opts.DeleteDisk.call = rejectExtraArgsFunc(directorFunc(func(director boshdir.Director) error {
		return NewDeleteDiskCmd(deps.UI, director).Run(opts.DeleteDisk)
	}))

	opts.Snapshots.call = rejectExtraArgsFunc(deploymentFunc(func(dep boshdir.Deployment) error {
		return NewSnapshotsCmd(deps.UI, dep).Run(opts.Snapshots)
	}))

	opts.TakeSnapshot.call = rejectExtraArgsFunc(deploymentFunc(func(dep boshdir.Deployment) error {
		return NewTakeSnapshotCmd(dep).Run(opts.TakeSnapshot)
	}))

	opts.DeleteSnapshot.call = rejectExtraArgsFunc(deploymentFunc(func(dep boshdir.Deployment) error {
		return NewDeleteSnapshotCmd(deps.UI, dep).Run(opts.DeleteSnapshot)
	}))

	opts.DeleteSnapshots.call = rejectExtraArgsFunc(deploymentFunc(func(dep boshdir.Deployment) error {
		return NewDeleteSnapshotsCmd(deps.UI, dep).Run()
	}))

	opts.BuildManifest.call = rejectExtraArgsFunc(globalOptsFunc(func() error {
		return NewBuildManifestCmd(deps.UI).Run(opts.BuildManifest)
	}))

	opts.CloudConfig.call = rejectExtraArgsFunc(directorFunc(func(director boshdir.Director) error {
		return NewCloudConfigCmd(deps.UI, director).Run()
	}))

	opts.UpdateCloudConfig.call = rejectExtraArgsFunc(directorFunc(func(director boshdir.Director) error {
		return NewUpdateCloudConfigCmd(deps.UI, director).Run(opts.UpdateCloudConfig)
	}))

	opts.RuntimeConfig.call = rejectExtraArgsFunc(directorFunc(func(director boshdir.Director) error {
		return NewRuntimeConfigCmd(deps.UI, director).Run()
	}))

	opts.UpdateRuntimeConfig.call = rejectExtraArgsFunc(directorFunc(func(director boshdir.Director) error {
		return NewUpdateRuntimeConfigCmd(deps.UI, director).Run(opts.UpdateRuntimeConfig)
	}))

	opts.Manifest.call = rejectExtraArgsFunc(deploymentFunc(func(dep boshdir.Deployment) error {
		return NewManifestCmd(deps.UI, dep).Run()
	}))

	opts.InspectRelease.call = rejectExtraArgsFunc(directorFunc(func(director boshdir.Director) error {
		return NewInspectReleaseCmd(deps.UI, director).Run(opts.InspectRelease)
	}))

	opts.VMs.call = rejectExtraArgsFunc(directorFunc(func(director boshdir.Director) error {
		return NewVMsCmd(deps.UI, director).Run(opts.VMs)
	}))

	opts.Instances.call = rejectExtraArgsFunc(deploymentFunc(func(dep boshdir.Deployment) error {
		return NewInstancesCmd(deps.UI, dep).Run(opts.Instances)
	}))

	opts.VMResurrection.call = rejectExtraArgsFunc(directorFunc(func(director boshdir.Director) error {
		return NewVMResurrectionCmd(director).Run(opts.VMResurrection)
	}))

	opts.Deploy.call = rejectExtraArgsFunc(directorAndDeploymentFunc(func(director boshdir.Director, dep boshdir.Deployment) error {
		uploadReleaseCmd := NewUploadReleaseCmd(nil, nil, nil, director, nil, deps.UI)
		return NewDeploy2Cmd(deps.UI, dep, uploadReleaseCmd).Run(opts.Deploy)
	}))

	opts.Start.call = rejectExtraArgsFunc(deploymentFunc(func(dep boshdir.Deployment) error {
		return NewStartCmd(deps.UI, dep).Run(opts.Start)
	}))

	opts.Stop.call = rejectExtraArgsFunc(deploymentFunc(func(dep boshdir.Deployment) error {
		return NewStopCmd(deps.UI, dep).Run(opts.Stop)
	}))

	opts.Restart.call = rejectExtraArgsFunc(deploymentFunc(func(dep boshdir.Deployment) error {
		return NewRestartCmd(deps.UI, dep).Run(opts.Restart)
	}))

	opts.Recreate.call = rejectExtraArgsFunc(deploymentFunc(func(dep boshdir.Deployment) error {
		return NewRecreateCmd(deps.UI, dep).Run(opts.Recreate)
	}))

	opts.CloudCheck.call = rejectExtraArgsFunc(deploymentFunc(func(dep boshdir.Deployment) error {
		return NewCloudCheckCmd(dep, deps.UI).Run(opts.CloudCheck)
	}))

	opts.CleanUp.call = rejectExtraArgsFunc(directorFunc(func(director boshdir.Director) error {
		return NewCleanUpCmd(deps.UI, director).Run(opts.CleanUp)
	}))

	opts.Logs.call = rejectExtraArgsFunc(directorAndDeploymentFunc(func(director boshdir.Director, dep boshdir.Deployment) error {
		downloader := NewUIDownloader(director, deps.SHA1Calc, deps.Time, deps.FS, deps.UI)
		sshProvider := boshssh.NewProvider(deps.CmdRunner, deps.FS, deps.UI, deps.Logger)
		nonIntSSHRunner := sshProvider.NewSSHRunner(false)
		return NewLogsCmd(dep, downloader, deps.UUIDGen, nonIntSSHRunner).Run(opts.Logs)
	}))

	opts.SSH.call = deploymentFunc(func(dep boshdir.Deployment) error {
		sshProvider := boshssh.NewProvider(deps.CmdRunner, deps.FS, deps.UI, deps.Logger)
		intSSHRunner := sshProvider.NewSSHRunner(true)
		nonIntSSHRunner := sshProvider.NewSSHRunner(false)
		resultsSSHRunner := sshProvider.NewResultsSSHRunner(false)
		return NewSSHCmd(dep, deps.UUIDGen, intSSHRunner, nonIntSSHRunner, resultsSSHRunner, deps.UI).Run(opts.SSH)
	})

	opts.SCP.call = rejectExtraArgsFunc(deploymentFunc(func(dep boshdir.Deployment) error {
		sshProvider := boshssh.NewProvider(deps.CmdRunner, deps.FS, deps.UI, deps.Logger)
		scpRunner := sshProvider.NewSCPRunner()
		return NewSCPCmd(dep, deps.UUIDGen, scpRunner, deps.UI).Run(opts.SCP)
	}))

	opts.ExportRelease.call = rejectExtraArgsFunc(directorAndDeploymentFunc(func(director boshdir.Director, dep boshdir.Deployment) error {
		downloader := NewUIDownloader(director, deps.SHA1Calc, deps.Time, deps.FS, deps.UI)
		return NewExportReleaseCmd(dep, downloader).Run(opts.ExportRelease)
	}))

	opts.InitRelease.call = rejectExtraArgsWithDirectoryFunc(releaseDirFunc(func(releaseDir boshreldir.ReleaseDir) error {
		return NewInitReleaseCmd(releaseDir).Run(opts.InitRelease)
	}))

	opts.ResetRelease.call = rejectExtraArgsWithDirectoryFunc(releaseDirFunc(func(releaseDir boshreldir.ReleaseDir) error {
		return NewResetReleaseCmd(releaseDir).Run(opts.ResetRelease)
	}))

	opts.GenerateJob.call = rejectExtraArgsWithDirectoryFunc(releaseDirFunc(func(releaseDir boshreldir.ReleaseDir) error {
		return NewGenerateJobCmd(releaseDir).Run(opts.GenerateJob)
	}))

	opts.GeneratePackage.call = rejectExtraArgsWithDirectoryFunc(releaseDirFunc(func(releaseDir boshreldir.ReleaseDir) error {
		return NewGeneratePackageCmd(releaseDir).Run(opts.GeneratePackage)
	}))

	opts.FinalizeRelease.call = rejectExtraArgsWithDirectoryFunc(func(dir DirOrCWDArg) error {
		return releaseProvidersFunc(func(relProv boshrel.Provider, relDirProv boshreldir.Provider) error {
			releaseReader := relDirProv.NewReleaseReader(dir.Path)
			releaseDir := relDirProv.NewFSReleaseDir(dir.Path)
			return NewFinalizeReleaseCmd(releaseReader, releaseDir, deps.UI).Run(opts.FinalizeRelease)
		})()
	})

	opts.CreateRelease.call = rejectExtraArgsWithDirectoryFunc(func(dir DirOrCWDArg) error {
		return releaseProvidersFunc(func(relProv boshrel.Provider, relDirProv boshreldir.Provider) error {
			releaseReader := relDirProv.NewReleaseReader(dir.Path)
			releaseDir := relDirProv.NewFSReleaseDir(dir.Path)
			return NewCreateReleaseCmd(releaseReader, releaseDir, deps.UI).Run(opts.CreateRelease)
		})()
	})

	opts.Blobs.call = rejectExtraArgsWithDirectoryFunc(blobsDirFunc(func(blobsDir boshreldir.BlobsDir) error {
		return NewBlobsCmd(blobsDir, deps.UI).Run()
	}))

	opts.AddBlob.call = rejectExtraArgsWithDirectoryFunc(blobsDirFunc(func(blobsDir boshreldir.BlobsDir) error {
		return NewAddBlobCmd(blobsDir, deps.FS, deps.UI).Run(opts.AddBlob)
	}))

	opts.RemoveBlob.call = rejectExtraArgsWithDirectoryFunc(blobsDirFunc(func(blobsDir boshreldir.BlobsDir) error {
		return NewRemoveBlobCmd(blobsDir, deps.UI).Run(opts.RemoveBlob)
	}))

	opts.UploadBlobs.call = rejectExtraArgsWithDirectoryFunc(blobsDirFunc(func(blobsDir boshreldir.BlobsDir) error {
		return NewUploadBlobsCmd(blobsDir).Run()
	}))

	opts.SyncBlobs.call = rejectExtraArgsWithDirectoryFunc(blobsDirFunc(func(blobsDir boshreldir.BlobsDir) error {
		return NewSyncBlobsCmd(blobsDir).Run()
	}))

	return Factory{opts: &opts}
}

func (f Factory) RunCommand(args []string) error {
	parser := goflags.NewParser(f.opts, goflags.HelpFlag|goflags.PassDoubleDash)

	_, err := parser.ParseArgs(args)

	return err
}
