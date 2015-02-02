package manifest

import (
	bmproperty "github.com/cloudfoundry/bosh-micro-cli/common/property"
)

type Manifest struct {
	Name            string
	Release         string
	RawProperties   map[interface{}]interface{}
	Mbus            string
	Registry        Registry
	AgentEnvService string
	SSHTunnel       SSHTunnel
}

type ReleaseJobRef struct {
	Name    string
	Release string
}

type Registry struct {
	Username string
	Password string
	Host     string
	Port     int
}

func (r Registry) IsEmpty() bool {
	return r == Registry{}
}

type SSHTunnel struct {
	User       string
	Host       string
	Port       int
	Password   string
	PrivateKey string `yaml:"private_key"`
}

func (o SSHTunnel) IsEmpty() bool {
	return o == SSHTunnel{}
}

func (m Manifest) Properties() (bmproperty.Map, error) {
	return bmproperty.BuildMap(m.RawProperties)
}
