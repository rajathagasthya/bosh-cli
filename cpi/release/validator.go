package release

import (
	biinstallmanifest "github.com/cloudfoundry/bosh-cli/v7/installation/manifest"
	bosherr "github.com/cloudfoundry/bosh-utils/errors"
)

const (
	ReleaseBinaryName = "bin/cpi"
)

type Validator struct {
}

func NewValidator() Validator {
	return Validator{}
}

func (v Validator) Validate(templates []biinstallmanifest.ReleaseJobRef, cpiInstaller *CpiInstaller) error {
	errs := []error{}
	releasePackagingErrs := []error{}
	releaseNamesInspected := []string{}
	numberCpiBinariesFound := 0

	for _, template := range templates {
		cpiReleaseName := template.Release
		cpiReleaseJobName := template.Name
		release, found := cpiInstaller.ReleaseManager.Find(cpiReleaseName)
		releaseNamesInspected = append(releaseNamesInspected, cpiReleaseName)

		if !found {
			releasePackagingErrs = append(releasePackagingErrs, bosherr.Errorf("installation release '%s' must refer to a provided release", cpiReleaseName))
			continue
		}

		job, ok := release.FindJobByName(cpiReleaseJobName)

		if !ok {
			releasePackagingErrs = append(releasePackagingErrs, bosherr.Errorf("release '%s' must contain specified job '%s'", cpiReleaseName, cpiReleaseJobName))
			continue
		}

		_, ok = job.FindTemplateByValue(ReleaseBinaryName)
		if ok {
			numberCpiBinariesFound += 1
		}
	}

	if numberCpiBinariesFound != 1 {
		errs = append(errs, bosherr.Errorf("Found %d releases containing a template that renders to target '%s'. Expected to find 1. Releases inspected: %v", numberCpiBinariesFound, ReleaseBinaryName, releaseNamesInspected))
		errs = append(errs, releasePackagingErrs...)
		return bosherr.NewMultiError(errs...)
	} else {
		return nil
	}
}
