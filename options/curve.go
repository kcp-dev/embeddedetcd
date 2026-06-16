/*
Copyright 2026 The KCP Authors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package options

import (
	"crypto/elliptic"
	"fmt"

	"github.com/spf13/pflag"
)

// ECDSACurve wraps an elliptic.Curve and implements pflag.Value for flag parsing.
type ECDSACurve struct {
	elliptic.Curve
}

var _ pflag.Value = (*ECDSACurve)(nil)

func (c *ECDSACurve) String() string {
	if c.Curve == nil {
		return ""
	}
	return c.Curve.Params().Name
}

func (c *ECDSACurve) Set(value string) error {
	switch value {
	case "P-224":
		c.Curve = elliptic.P224()
	case "P-256":
		c.Curve = elliptic.P256()
	case "P-384":
		c.Curve = elliptic.P384()
	case "P-521":
		c.Curve = elliptic.P521()
	default:
		return fmt.Errorf("must be one of: P-224, P-256, P-384, P-521")
	}
	return nil
}

func (c *ECDSACurve) Type() string {
	return "ECDSACurve"
}
