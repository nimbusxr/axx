// SPDX-License-Identifier: Apache-2.0

package rest

import (
	"crypto/tls"
	"errors"
	"fmt"
)

// unverified says, of a request that failed because its host's certificate does not verify, how
// a run accepts it: a service under test often has a certificate of its own, and the failure
// alone does not name the setting.
func unverified(err error) error {
	var cert *tls.CertificateVerificationError
	if !errors.As(err, &cert) {
		return err
	}
	return fmt.Errorf("%w (certificates are verified: in axx.yaml, packs.rest.tls.verify: false accepts a service's certificate whatever it is)", err)
}
