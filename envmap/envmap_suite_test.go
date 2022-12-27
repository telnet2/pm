package envmap

import (
	"testing"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

func TestEnvMap(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "EnvMap Suite")
}
