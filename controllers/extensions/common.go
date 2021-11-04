package extensions

import (
	"context"
	"crypto/sha256"
	"fmt"

	gardenerv1beta1constants "github.com/gardener/gardener/pkg/apis/core/v1beta1/constants"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

const fieldOwner = client.FieldOwner("gardener-extension-kind")

func SHA256(data []byte) string {
	return fmt.Sprintf("%x", sha256.Sum256(data))
}

func removeOperationAnnotation(ctx context.Context, w client.Writer, obj client.Object) (bool, error) {
	ann := obj.GetAnnotations()
	if ann[gardenerv1beta1constants.GardenerOperation] == gardenerv1beta1constants.GardenerOperationReconcile {
		patch := client.MergeFrom(obj.DeepCopyObject().(client.Object))
		delete(ann, gardenerv1beta1constants.GardenerOperation)
		obj.SetAnnotations(ann)
		if err := w.Patch(ctx, obj, patch); err != nil {
			return false, fmt.Errorf("failed to remove operation annation: %w", err)
		}
		return true, nil
	}

	return false, nil
}
