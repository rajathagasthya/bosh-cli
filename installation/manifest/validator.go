package manifest

import (
	"strings"

	bosherr "github.com/cloudfoundry/bosh-utils/errors"
	boshlog "github.com/cloudfoundry/bosh-utils/logger"

	birelsetmanifest "github.com/cloudfoundry/bosh-cli/v7/release/set/manifest"
)

type Validator interface {
	Validate(Manifest, birelsetmanifest.Manifest) error
}

type validator struct {
	logger boshlog.Logger
}

func NewValidator(logger boshlog.Logger) Validator {
	return &validator{
		logger: logger,
	}
}

func (v *validator) Validate(manifest Manifest, releaseSetManifest birelsetmanifest.Manifest) error {
	errs := []error{}

	if len(manifest.Templates) == 0 {
		if err := v.validateReleaseJobRef(manifest.Template, releaseSetManifest); err != nil {
			err = append(err, bosherr.Errorf("valid manifest.templates or manifest.template (deprecated) must be specified"))
			return bosherr.NewMultiError(err...)
		}
	}

	for _, template := range manifest.Templates {
		errRet := v.validateReleaseJobRef(template, releaseSetManifest)
		errs = append(errs, errRet...)
	}

	if len(errs) > 0 {
		return bosherr.NewMultiError(errs...)
	}

	return nil
}

func (v *validator) validateReleaseJobRef(releaseJobRef ReleaseJobRef, releaseSetManifest birelsetmanifest.Manifest) []error {
	errs := []error{}
	jobName := releaseJobRef.Name
	if v.isBlank(jobName) {
		errs = append(errs, bosherr.Error("cloud_provider.template.name must be provided"))
	}

	releaseName := releaseJobRef.Release
	if v.isBlank(releaseName) {
		errs = append(errs, bosherr.Error("cloud_provider.template.release must be provided"))
	}

	_, found := releaseSetManifest.FindByName(releaseName)
	if !found {
		errs = append(errs, bosherr.Errorf("cloud_provider.template.release '%s' must refer to a release in releases", releaseName))
	}
	return errs
}

func (v *validator) isBlank(str string) bool {
	return str == "" || strings.TrimSpace(str) == ""
}
