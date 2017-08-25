/*
Copyright 2016 The Archon Authors.

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

package cloudprovider

import (
	"errors"
	"k8s.io/kubernetes/pkg/cloudprovider"
)

// Cloud provider interface
type Provider interface {
	cloudprovider.Interface
	Volume() (Volume, bool)
}

type ProviderFactory func() (Provider, error)

var (
	providers = make(map[string]ProviderFactory)
)

func RegisterProvider(name string, p ProviderFactory) {
	providers[name] = p
}

func GetProvider(name string) (Provider, error) {
	if p, ok := providers[name]; ok {
		return p()
	}

	return nil, errors.New("No such provider: " + name)

}
