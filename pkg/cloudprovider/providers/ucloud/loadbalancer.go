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
	"strings"
	"time"

	log "github.com/golang/glog"
	"github.com/ucloud/ucloud-sdk-go/service/uhost"
	"github.com/ucloud/ucloud-sdk-go/service/ulb"
	"github.com/ucloud/ucloud-sdk-go/service/unet"
	api "k8s.io/kubernetes/pkg/api/v1"
	"kubeup.com/kube-ucloud/pkg/util"
)

// Options can be set as annotations on services with the provider prefix.
// ex. ucloud.archon.kubeup.com/network-type: internet
// Check ./consts.go or ucloud api documentation for possible values.
type UCloudLoadBalancerOptions struct {
	// Network type: internet/internal
	NetworkType string `k8s:"network-type"`
	ChargeType  string `k8s:"charge-type"`
	ListenType  string `k8s:"listen-type"`
	Method      string `k8s:"method"`

	// Use existing eip (value is eip id)
	UseEIP string `k8s:"use-eip"`
	// EIP related
	EIPOperatorName      string `k8s:"eip-operator-name"`
	EIPBandwidth         int    `k8s:"eip-bandwidth"`
	EIPChargeType        string `k8s:"eip-charge-type"`
	EIPPayMode           string `k8s:"eip-pay-mode"`
	EIPSharedBandwidthID string `k8s:"eip-shared-bandwidth-id"`

	// Health check timeout
	ClientTimeout int `k8s:"client-timeout"`
}

var (
	DefaultOptions UCloudLoadBalancerOptions = UCloudLoadBalancerOptions{
		NetworkType:     "internet",
		EIPBandwidth:    2,
		EIPOperatorName: EIPOperatorBGP,
		EIPPayMode:      EIPPayModeTraffic,
		EIPChargeType:   EIPChargeTypeDynamic,
	}
)

// Loadbalancer helpers
func lbToStatus(lb *ulb.ULBSet) *api.LoadBalancerStatus {
	ret := &api.LoadBalancerStatus{}
	for _, ip := range lb.IPSet {
		ret.Ingress = append(ret.Ingress, api.LoadBalancerIngress{
			IP: ip.EIP,
		})
	}
	if lb.PrivateIP != "" {
		ret.Ingress = append(ret.Ingress, api.LoadBalancerIngress{
			IP: lb.PrivateIP,
		})
	}
	return ret
}

// Loadbalancer interface
func (p *UCloudProvider) GetLoadBalancerName(service *api.Service) string {
	ret := fmt.Sprintf("%s.%s", service.Namespace, service.Name)
	return ret
}

func (p *UCloudProvider) getEIPNameForLoadBalancer(lb *ulb.ULBSet) string {
	return fmt.Sprintf("LBEIP-%s", lb.ULBId)
}

func (p *UCloudProvider) getEIPByName(name string) (*unet.EIPSet, error) {
	offset := 0
	limit := 40
	count := 0
	for {
		resp, err := p.unet.DescribeEIP(&unet.DescribeEIPParams{
			Region: p.region,
			// TODO: ProjectId: project,
			Offset: offset,
			Limit:  limit,
		})
		if err != nil {
			return nil, fmt.Errorf("Error getting EIP by name %s: %v", name, err)
		}

		if resp.EIPSet == nil {
			return nil, fmt.Errorf("EIP not found: %s", name)
		}

		for _, eip := range *resp.EIPSet {
			if eip.Name == name {
				return &eip, nil
			}
			count += 1
		}

		if count >= resp.TotalCount {
			break
		}

		offset += len(*resp.EIPSet)
	}

	return nil, fmt.Errorf("EIP not found: %s", name)
}

func (p *UCloudProvider) getLoadBalancer(name string) (*ulb.ULBSet, bool, error) {
	args := &ulb.DescribeULBParams{
		Region:    p.region,
		ProjectId: p.project,
	}

	resp, err := p.ulb.DescribeULB(args)
	if err != nil {
		log.Errorf("Error describing load balancer: %+v", err)
		return nil, false, err
	}

	for _, lb := range resp.DataSet {
		if lb.Name == name {
			return &lb, true, nil
		}
	}

	return nil, false, nil
}

func (p *UCloudProvider) GetLoadBalancer(clusterName string, service *api.Service) (status *api.LoadBalancerStatus, exists bool, err error) {
	name := p.GetLoadBalancerName(service)
	lb := (*ulb.ULBSet)(nil)
	lb, exists, err = p.getLoadBalancer(name)
	if lb != nil {
		status = lbToStatus(lb)
	}
	return
}

func (p *UCloudProvider) createLoadBalancer(name string, lbOptions *UCloudLoadBalancerOptions) (lb *ulb.ULBSet, err error) {
	var allocatedEIP *unet.EIPSet
	defer func() {
		if lb != nil && lb.ULBId != "" && err != nil {
			if _, err2 := p.ulb.DeleteULB(&ulb.DeleteULBParams{
				Region:    p.region,
				ProjectId: p.project,
				ULBId:     lb.ULBId,
			}); err2 != nil {
				log.Errorf("Unable to revert lb creation: %s %s", lb.Name, lb.ULBId)
			}
		}

		if allocatedEIP != nil && err != nil {
			if _, err2 := p.unet.ReleaseEIP(&unet.ReleaseEIPParams{
				Region: p.region,
				//ProjectId: p.project,
				EIPId: allocatedEIP.EIPId,
			}); err2 != nil {
				log.Errorf("Unable to revert eip creation: %s %s", lb.Name, allocatedEIP.EIPId)
			}
		}
	}()

	inner := "No"
	outer := "Yes"
	if lbOptions.NetworkType == "internal" {
		inner = "Yes"
		outer = "No"
	}

	args := &ulb.CreateULBParams{
		Region:     p.region,
		ProjectId:  p.project,
		ULBName:    name,
		InnerMode:  inner,
		OuterMode:  outer,
		ChargeType: lbOptions.ChargeType,
	}
	_, err = p.ulb.CreateULB(args)
	if err != nil {
		log.Errorf("Error creating load balancer %s: %+v", name, err)
		return
	}

	retry := 10
	for retry > 0 {
		time.Sleep(time.Duration(5) * time.Second)
		lb, _, err = p.getLoadBalancer(name)
		if lb != nil {
			log.Infof("Created lb %+v with args %+v", lb, args)
			break
		}
		if err != nil {
			log.Warningf("Error checking if creating load balancer has succeeded: %v. Will retry", err)
		}

		retry -= 1
	}

	if lb == nil && retry <= 0 {
		if err == nil {
			log.Errorf("LB just doesn't exist.")
			err = fmt.Errorf("LB doesn't exist after be created. Maybe it takes too long")
		}
		return
	}

	if lbOptions.NetworkType == "internal" {
		return
	}

	// Allocate eip
	eipId := lbOptions.UseEIP
	if lbOptions.UseEIP == "" {
		eipResp, err := p.unet.AllocateEIP(&unet.AllocateEIPParams{
			Region: p.region,
			// TODO: ProjectId: p.project,
			OperatorName:     lbOptions.EIPOperatorName,
			Bandwidth:        lbOptions.EIPBandwidth,
			ChargeType:       lbOptions.EIPChargeType,
			PayMode:          lbOptions.EIPPayMode,
			ShareBandwidthId: lbOptions.EIPSharedBandwidthID,
			Name:             p.getEIPNameForLoadBalancer(lb),
		})

		if err != nil || eipResp.EIPSet == nil || len(*eipResp.EIPSet) == 0 {
			err = fmt.Errorf("Unable to allocate eip for LB %s: %v", name, err)
			return lb, err
		}

		allocatedEIP = &(*eipResp.EIPSet)[0]
		eipId = allocatedEIP.EIPId
	}

	_, err = p.unet.BindEIP(&unet.BindEIPParams{
		Region: p.region,
		//TODO: ProjectId:    p.project,
		EIPId:        eipId,
		ResourceType: ResourceTypeULB,
		ResourceId:   lb.ULBId,
	})

	if err != nil {
		return
	}

	lb, _, err = p.getLoadBalancer(name)

	return
}

func (p *UCloudProvider) EnsureLoadBalancer(clusterName string, service *api.Service, nodes []*api.Node) (*api.LoadBalancerStatus, error) {
	spec := service.Spec
	name := p.GetLoadBalancerName(service)

	// Check services
	if spec.SessionAffinity != api.ServiceAffinityNone {
		return nil, fmt.Errorf("unsupported load balancer affinity: %v", spec.SessionAffinity)
	}

	for _, port := range spec.Ports {
		switch port.Protocol {
		case api.ProtocolTCP, api.ProtocolUDP:
			continue
		default:
			return nil, fmt.Errorf("Unsupported server port protocol for UCloud load balancers: %v", port.Protocol)
		}
	}

	if spec.LoadBalancerIP != "" {
		return nil, fmt.Errorf("LoadBalancerIP can't be set for UCloud load balancers")
	}

	instances, err := p.getInstancesByNodes(nodes)
	if err != nil {
		return nil, err
	}

	if len(instances) != len(nodes) {
		log.V(1).Infof("Instances don't match nodes. Instances: %+v Nodes: %+v", instances, nodes)
	}

	ids := []string{}
	for _, ins := range instances {
		ids = append(ids, ins.UHostId)
	}
	log.V(2).Infof("Ensuring loadbalancer with backends %+v", ids)

	lbOptions := DefaultOptions
	if service.Annotations != nil {
		err = util.MapToStruct(service.Annotations, &lbOptions, UCloudAnnotationPrefix)
		if err != nil {
			log.Warningf("Unable to extract loadbalancer options from service annotations")
		}
	}

	lb, _, err := p.getLoadBalancer(name)
	if err != nil {
		log.Errorf("Error ensuring load balancer: %v", err)
		return nil, err
	}

	if lb == nil {
		lb, err = p.createLoadBalancer(name, &lbOptions)
		if err != nil {
			return nil, err
		}
	}

	// Sync lb
	err = p.ensureLBListeners(lb, spec.Ports, lbOptions)
	if err != nil {
		return nil, err
	}

	lb, _, err = p.getLoadBalancer(name)
	if err != nil {
		return nil, err
	}

	err = p.ensureLBBackends(lb, spec.Ports, instances, lbOptions)
	if err != nil {
		return nil, err
	}

	return lbToStatus(lb), nil
}

func (p *UCloudProvider) ensureLBListeners(lb *ulb.ULBSet, ports []api.ServicePort, lbOptions UCloudLoadBalancerOptions) error {
	keyFmt := "%d|%s"
	expected := make(map[string]api.ServicePort)
	actual := lb.VServerSet[:]
	for _, p := range ports {
		if p.NodePort == 0 {
			log.Infof("Ignored a service port with no NodePort syncing listeners: %+v", p)
			continue
		}
		expected[fmt.Sprintf(keyFmt, p.Port, strings.ToLower(string(p.Protocol)))] = p
	}

	// Diff of port, protocol and nodeport
	removals := []string{}
	for _, listener := range actual {
		key := fmt.Sprintf(keyFmt, listener.FrontendPort, strings.ToLower(string(listener.Protocol)))
		log.V(4).Infof("Existing listener: %+v", key)
		if _, ok := expected[key]; ok {
			delete(expected, key)
			continue
		}

		removals = append(removals, listener.VServerId)
	}

	log.V(2).Infof("Existing: %+v, Removing %v, creating %+v", actual, removals, expected)

	if len(removals) > 0 {
		for _, vserverId := range removals {
			_, err := p.ulb.DeleteVServer(&ulb.DeleteVServerParams{
				Region:    p.region,
				ProjectId: p.project,
				ULBId:     lb.ULBId,
				VServerId: vserverId,
			})
			if err != nil {
				log.Errorf("Error deleting load balancer listener: %+v", err)
				return err
			}
		}
	}

	if len(expected) > 0 {
		for _, sp := range expected {
			var err error
			args := &ulb.CreateVServerParams{
				Region:        p.region,
				ProjectId:     p.project,
				ULBId:         lb.ULBId,
				ListenType:    ulb.VServerListenType(lbOptions.ListenType),
				FrontendPort:  int(sp.Port),
				Method:        lbOptions.Method,
				ClientTimeout: lbOptions.ClientTimeout,
			}

			switch sp.Protocol {
			case api.ProtocolTCP:
				args.Protocol = ulb.VServerProtocolTCP
			case api.ProtocolUDP:
				args.Protocol = ulb.VServerProtocolUDP
			default:
				err = fmt.Errorf("Error creating service listener. Unsupported listener protocol: %s", string(sp.Protocol))
			}

			_, err = p.ulb.CreateVServer(args)
			if err != nil {
				log.Errorf("Error creating load balancer listener for service port %+v: %+v", sp, err)
				return err
			}
		}
	}

	return nil
}

func (p *UCloudProvider) ensureLBBackends(lb *ulb.ULBSet, sp []api.ServicePort, instances []uhost.UHostSet, lbOptions UCloudLoadBalancerOptions) error {
	for _, vserver := range lb.VServerSet {

		servicePort := (*api.ServicePort)(nil)
		for _, p := range sp {
			if p.Port == int32(vserver.FrontendPort) {
				servicePort = &p
				break
			}
		}

		if servicePort == nil {
			log.Warningf("Unable to find corresponding service port for listener %+v. It should have been removed. Ignoring.", vserver)
			continue
		}

		port := int(servicePort.NodePort)
		if ulb.VServerListenType(lbOptions.ListenType) == ulb.VServerListenTypePacketsTransmit {
			port = int(servicePort.Port)
		}

		actual := vserver.BackendSet[:]
		expected := make(map[string]bool)
		for _, ins := range instances {
			expected[ins.UHostId] = true
		}

		removals := []string{}
		for _, s := range actual {
			if _, ok := expected[s.ResourceId]; s.ResourceType == ULBResourceTypeUHost && s.Port == port && ok {
				delete(expected, s.ResourceId)
				continue
			}

			removals = append(removals, s.BackendId)
		}

		log.V(2).Infof("Backends for vserver %s.\n Existing: %+v, Removing %v, creating %+v", vserver.VServerId, actual, removals, expected)

		for ins := range expected {
			_, err := p.ulb.AllocateBackend(&ulb.AllocateBackendParams{
				Region:       p.region,
				ProjectId:    p.project,
				ULBId:        lb.ULBId,
				VServerId:    vserver.VServerId,
				ResourceType: ULBResourceTypeUHost,
				ResourceId:   ins,
				Port:         port,
			})

			if err != nil {
				log.Errorf("Error adding backend servers from load balancer %s: %+v", lb.ULBId, err)
				return err
			}
		}

		for _, backendId := range removals {
			_, err := p.ulb.ReleaseBackend(&ulb.ReleaseBackendParams{
				Region:    p.region,
				ProjectId: p.project,
				ULBId:     lb.ULBId,
				BackendId: backendId,
			})
			if err != nil {
				log.Errorf("Error removing backend servers from load balancer %s backend %s: %+v", lb.ULBId, backendId, err)
				return err
			}
		}
	}

	return nil
}

func (p *UCloudProvider) UpdateLoadBalancer(clusterName string, service *api.Service, nodes []*api.Node) error {
	lb, _, err := p.getLoadBalancer(p.GetLoadBalancerName(service))
	if err != nil {
		return err
	}

	if lb == nil {
		return fmt.Errorf("Load balancer is not found")
	}

	lbOptions := DefaultOptions
	if service.Annotations != nil {
		err = util.MapToStruct(service.Annotations, &lbOptions, UCloudAnnotationPrefix)
		if err != nil {
			log.Warningf("Unable to extract loadbalancer options from service annotations")
		}
	}

	// Sync lb
	log.Infof("Updating LB: %+v", lb)
	err = p.ensureLBListeners(lb, service.Spec.Ports, lbOptions)
	if err != nil {
		return err
	}

	instances, err := p.getInstancesByNodes(nodes)
	if err != nil {
		return err
	}

	err = p.ensureLBBackends(lb, service.Spec.Ports, instances, lbOptions)
	if err != nil {
		return err
	}

	return nil
}

func (p *UCloudProvider) EnsureLoadBalancerDeleted(clusterName string, service *api.Service) error {
	name := p.GetLoadBalancerName(service)
	log.Infof("Deleting service lb: %s", name)

	lb, _, err := p.getLoadBalancer(name)
	if err != nil {
		return err
	}

	if lb == nil {
		log.Infof("Probably already gone. Ignoring")
		return nil
	}

	_, err = p.ulb.DeleteULB(&ulb.DeleteULBParams{
		Region:    p.region,
		ProjectId: p.project,
		ULBId:     lb.ULBId,
	})
	if err != nil {
		return fmt.Errorf("Unable to delete ULB %s: %v", lb.ULBId, err)
	}

	eip, err := p.getEIPByName(p.getEIPNameForLoadBalancer(lb))
	if err != nil || eip == nil {
		log.Infof("Unable to find EIP for ULB %s. Will Ignore: %v", lb.ULBId, err)
		return nil
	}

	_, err = p.unet.ReleaseEIP(&unet.ReleaseEIPParams{
		Region: p.region,
		// TODO: ProjectId: p.project,
		EIPId: eip.EIPId,
	})
	if err != nil {
		err = fmt.Errorf("Unable to release EIP %s: %v", eip.EIPId, err)
	}

	return err
}
