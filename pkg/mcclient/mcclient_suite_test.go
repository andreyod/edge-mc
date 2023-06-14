package mcclient

import (
	"testing"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

func TestMcclient(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Mcclient Suite")
}
