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

package ucloud

import (
	"fmt"
	"os"

	"github.com/ucloud/ucloud-sdk-go/service/uhost"
	"github.com/ucloud/ucloud-sdk-go/service/ulb"
	"github.com/ucloud/ucloud-sdk-go/service/unet"
	origucloud "github.com/ucloud/ucloud-sdk-go/ucloud"
	"github.com/ucloud/ucloud-sdk-go/ucloud/auth"
	origcloudprovider "k8s.io/kubernetes/pkg/cloudprovider"
	"k8s.io/kubernetes/pkg/controller"
	"kubeup.com/kube-ucloud/pkg/cloudprovider"
)

const (
	UCloudAnnotationPrefix = "ucloud.archon.kubeup.com/"
	ProviderName           = "ucloud"
)

type UCloudProvider struct {
	region       string
	zone         string
	project      string
	hostname     string
	nodeNameType NodeNameType

	ulb   *ulb.ULB
	uhost *uhost.UHost
	unet  *unet.UNet
}

// NodeName describes the format of name for nodes in the cluster
type NodeNameType string

const (
	// Use private ip address as the node name
	NodeNameTypePrivateIP NodeNameType = "private-ip"
	// Use hostname as the node name. This is the default behavior for k8s
	NodeNameTypeHostname NodeNameType = "hostname"
)

var _ origcloudprovider.Interface = &UCloudProvider{}

func init() {
	cloudprovider.RegisterProvider(ProviderName, NewProvider)
}

func NewProvider() (cloudprovider.Provider, error) {
	publicKey := os.Getenv("UCLOUD_PUBLIC_KEY")
	privateKey := os.Getenv("UCLOUD_PRIVATE_KEY")
	if publicKey == "" || privateKey == "" {
		return nil, fmt.Errorf("UCLOUD_PUBLIC_KEY or UCLOUD_PRIVATE_KEY is not set")
	}

	zone := os.Getenv("UCLOUD_ZONE")
	if zone == "" {
		return nil, fmt.Errorf("UCLOUD_ZONE is not specified")
	}
	region := os.Getenv("UCLOUD_REGION")
	if region == "" {
		return nil, fmt.Errorf("UCLOUD_REGION is not specified")
	}

	project := os.Getenv("UCLOUD_PROJECT")
	if project == "" {
		return nil, fmt.Errorf("UCLOUD_PROJECT is not specified")
	}

	var nodeNameType NodeNameType
	switch NodeNameType(os.Getenv("UCLOUD_NODE_NAME_TYPE")) {
	case NodeNameTypePrivateIP:
		nodeNameType = NodeNameTypePrivateIP
	case NodeNameTypeHostname:
		nodeNameType = NodeNameTypeHostname
	default:
		// Default to NodeNameTypePrivateIP for backward compatibility
		nodeNameType = NodeNameTypePrivateIP
	}
	hostname, _ := os.Hostname()

	config := &origucloud.Config{
		Credentials: &auth.KeyPair{
			PublicKey:  publicKey,
			PrivateKey: privateKey,
		},
		Region:    region,
		ProjectID: project,
	}

	return &UCloudProvider{
		hostname:     hostname,
		nodeNameType: nodeNameType,
		zone:         zone,
		region:       region,
		project:      project,
		ulb:          ulb.New(config),
		uhost:        uhost.New(config),
		unet:         unet.New(config),
	}, nil
}

func (p *UCloudProvider) Initialize(clientBuilder controller.ControllerClientBuilder) {}

func (p *UCloudProvider) Clusters() (origcloudprovider.Clusters, bool) {
	return nil, false
}

func (p *UCloudProvider) Zones() (origcloudprovider.Zones, bool) {
	return p, true
}

func (p *UCloudProvider) Instances() (origcloudprovider.Instances, bool) {
	return nil, false
}

func (p *UCloudProvider) ProviderName() string {
	return ProviderName
}

func (p *UCloudProvider) Routes() (origcloudprovider.Routes, bool) {
	return nil, true
}

func (p *UCloudProvider) LoadBalancer() (origcloudprovider.LoadBalancer, bool) {
	return p, true
}

func (p *UCloudProvider) Volume() (cloudprovider.Volume, bool) {
	return nil, true
}

func (p *UCloudProvider) ScrubDNS(nameservers, searches []string) (nsOut, srchOut []string) {
	return
}
