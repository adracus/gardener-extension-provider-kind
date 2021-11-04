package extensions

import (
	"crypto/sha256"
	"fmt"

	"sigs.k8s.io/controller-runtime/pkg/client"
)

const fieldOwner = client.FieldOwner("gardener-extension-kind")

func SHA256(data []byte) string {
	return fmt.Sprintf("%x", sha256.Sum256(data))
}
