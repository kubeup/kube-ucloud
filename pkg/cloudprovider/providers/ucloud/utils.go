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
	"errors"
	"fmt"

	"github.com/golang/glog"
	"github.com/ucloud/ucloud-sdk-go/service/uhost"
	api "k8s.io/kubernetes/pkg/api/v1"
)

func getUHostPrivateIP(instance uhost.UHostSet) string {
	for _, ip := range instance.IPSet {
		if ip.Type == InstanceIPTypePrivate {
			return ip.IP
		}
	}
	return ""
}

func getNodePrivateIP(node *api.Node) string {
	for _, ip := range node.Status.Addresses {
		if ip.Type == api.NodeInternalIP {
			return ip.Address
		}
	}
	return ""
}

func (w *UCloudProvider) getInstances() (instances []uhost.UHostSet, err error) {
	offset := 0
	limit := 40
	for {
		args := &uhost.DescribeUHostInstanceParams{
			Region: w.region,
			Offset: offset,
			Limit:  limit,
		}

		resp, err := w.uhost.DescribeUHostInstance(args)
		if err != nil {
			return nil, fmt.Errorf("Unable to getInstances: %v", err)
		}

		instances = append(instances, resp.UHostSet...)

		if len(instances) >= resp.TotalCount {
			return instances, nil
		}

		offset += limit
	}

	return
}

func (p *UCloudProvider) mapInstanceToNodeName(instance uhost.UHostSet) string {
	switch p.nodeNameType {
	case NodeNameTypePrivateIP:
		return getUHostPrivateIP(instance)
	case NodeNameTypeHostname:
		return instance.Name
	}
	return instance.Name
}

func (p *UCloudProvider) getInstanceByNodeName(nodeName string) (instance uhost.UHostSet, err error) {
	instances, err := p.getInstances()
	if err != nil {
		return
	}

	glog.Infof("instances: %+v", instances)

	switch p.nodeNameType {
	case NodeNameTypePrivateIP:
		instances = filter(instances, byPrivateIP(nodeName))
	case NodeNameTypeHostname:
		instances = filter(instances, byHostname(nodeName))
	default:
		instances = []uhost.UHostSet{}
	}

	if len(instances) == 0 {
		err = errors.New("Unable to get instance of node:" + nodeName)
		return
	}

	if len(instances) > 1 {
		err = errors.New("Multiple instance with the same node name:" + nodeName)
		return
	}

	return instances[0], nil
}

func (p *UCloudProvider) getInstancesByNodeNames(nodeNames []string) (results []uhost.UHostSet, err error) {
	instances, err := p.getInstances()
	if err != nil {
		return
	}

	var by func(string) func(uhost.UHostSet) bool
	switch p.nodeNameType {
	case NodeNameTypePrivateIP:
		by = byPrivateIP
	case NodeNameTypeHostname:
		by = byHostname
	default:
		by = byEmpty
	}

	glog.Infof("instances: %+v", instances)

	for _, nodeName := range nodeNames {
		instances = filter(instances, by(nodeName))

		if len(instances) == 0 {
			return nil, errors.New("Unable to get instance of node:" + nodeName)
		}

		if len(instances) > 1 {
			return nil, errors.New("Multiple instance with the same node name:" + nodeName)
		}
		results = append(results, instances[0])
	}

	return
}

func (p *UCloudProvider) getInstancesByNodes(nodes []*api.Node) (results []uhost.UHostSet, err error) {
	instances, err := p.getInstances()
	if err != nil {
		return
	}

	kv := make(map[string]uhost.UHostSet)
	for _, ins := range instances {
		privateIP := getUHostPrivateIP(ins)
		if privateIP == "" {
			glog.V(2).Infof("Instance doesn't have private ip: %v", ins.UHostId)
			continue
		}

		kv[privateIP] = ins
	}

	for _, node := range nodes {
		nodeIP := getNodePrivateIP(node)
		if ins, ok := kv[nodeIP]; ok {
			results = append(results, ins)
		}
	}

	return
}

func filter(instances []uhost.UHostSet, f func(uhost.UHostSet) bool) (ret []uhost.UHostSet) {
	for _, i := range instances {
		if f(i) {
			ret = append(ret, i)
		}
	}
	return
}

func byHostname(hostname string) func(uhost.UHostSet) bool {
	return func(i uhost.UHostSet) bool {
		return i.Name == hostname
	}
}

func byPrivateIP(ip string) func(uhost.UHostSet) bool {
	return func(i uhost.UHostSet) bool {
		for _, s := range i.IPSet {
			if s.IP == ip && s.Type == InstanceIPTypePrivate {
				return true
			}
		}
		return false
	}
}

func byInstanceId(id string) func(uhost.UHostSet) bool {
	return func(i uhost.UHostSet) bool {
		return i.UHostId == id
	}
}

func byEmpty(string) func(uhost.UHostSet) bool {
	return func(uhost.UHostSet) bool {
		return false
	}
}
