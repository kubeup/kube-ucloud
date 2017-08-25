Kube UCloud
===========

[![CircleCI](https://circleci.com/gh/kubeup/kube-ucloud/tree/master.svg?style=shield)](https://circleci.com/gh/kubeup/kube-ucloud)

UCloud essentials for Kubernetes. Currently it provides ULB loadbalancer support.

Features
--------

* Service load balancers sync (TCP & UDP)

Docker Image
------------

- kubeup/kube-ucloud
- registry.aliyuncs.com/kubeup/kube-ucloud

Dependency
--------

Kubernetes 1.6+


Components
----------

**ucloud-controller** is a daemon responsible for service synchronization. It 
has to run on all master nodes.

Deploy to UCloud
----------------

### ucloud-controller

1. Update the required fields in `manifests/ucloud-controller.yaml`
2. Upload it to `pod-manifest-path-of-kubelets` on all your master nodes
3. Use docker logs to check if the controller is running properly

Usage
-----

### Services

Just create Loadbalancer Services as usual. Currently only TCP & UDP types are
supported. Some options can be customized through annotaion on Service. Please
see [pkg/cloudprovider/providers/ucloud/loadbalancer.go](https://github.com/kubeup/kube-ucloud/blob/master/pkg/cloudprovider/providers/ucloud/loadbalancer.go) for details.

License
-------

Apache Version 2.0
