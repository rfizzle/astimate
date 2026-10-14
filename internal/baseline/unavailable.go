package baseline

import (
	"errors"
	"fmt"
	"strings"

	"github.com/rfizzle/astimate/internal/metrics"
)

// CollectUnavailable returns, with ext's metrics.Skipper, the packages of
// the module at root that failed to load, keyed by module-relative
// directory (PackageDir, with modulePath the module path read at root),
// each with the reason it could not be measured: the kind of failure and
// its first error on one line, naming files relative to root. Call it
// after Collect on the same root, so ext answers from the load it just
// did. It returns nil, and no error, when ext is not a Skipper or nothing
// failed.
func CollectUnavailable(ext metrics.Extractor, root, modulePath string) (map[string]string, error) {
	sk, ok := ext.(metrics.Skipper)
	if !ok {
		return nil, nil
	}
	list, err := sk.Skipped(root)
	if err != nil {
		return nil, fmt.Errorf("listing the packages that fail to load: %w", err)
	}
	if len(list) == 0 {
		return nil, nil
	}
	out := make(map[string]string, len(list))
	for _, s := range list {
		msg := TreeRelative(errors.New(s.Error), root).Error()
		out[PackageDir(modulePath, s.Package)] = s.Reason + ": " + strings.Join(strings.Fields(msg), " ")
	}
	return out, nil
}
