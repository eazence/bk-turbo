/*
 * Copyright (c) 2021 THL A29 Limited, a Tencent company. All rights reserved
 *
 * This source code file is licensed under the MIT License, you may obtain a copy of the License at
 *
 * http://opensource.org/licenses/MIT
 *
 */

package k8s

import (
	"context"
	"crypto/tls"
	"fmt"
	"net"
	"net/http"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/TencentBlueKing/bk-turbo/src/backend/booster/common/blog"
	"github.com/TencentBlueKing/bk-turbo/src/backend/booster/common/codec"
	"github.com/TencentBlueKing/bk-turbo/src/backend/booster/common/http/httpclient"
	"github.com/TencentBlueKing/bk-turbo/src/backend/booster/server/config"
	op "github.com/TencentBlueKing/bk-turbo/src/backend/booster/server/pkg/resource/crm/operator"

	"github.com/ghodss/yaml"
	appsV1 "k8s.io/api/apps/v1"
	coreV1 "k8s.io/api/core/v1"
	metaV1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/fields"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
)

var (
	//EnableBCSApiGw define
	EnableBCSApiGw = ""
)

// define const vars
const (
	bcsAPIK8SBaseURI   = "%s/tunnels/clusters/%s/"
	bcsAPIGWK8SBaseURI = "%s/clusters/%s/"
	bcsAPIFederatedURI = "%s/clusters/%s/apis/federated.bkbcs.tencent.com/v1/namespaces/%s/availableresource"
	FederationCluster  = "Federation"

	disableLabel = "tbs/disabled"
	appLabel     = "tbs/name"

	specificPort = 31000

	envKeyHostPort = "HOST_PORT_"
	envKeyRandPort = "RAND_PORT_"

	reqTimeoutSecs  = 10 // 超时时间设置为10s
	reqSlowWarnSecs = 3  //慢查询告警时间设置为3s

	templateVarImage            = "__crm_image__"
	templateVarName             = "__crm_name__"
	templateVarNamespace        = "__crm_namespace__"
	templateVarInstance         = "__crm_instance__"
	templateVarCPU              = "__crm_cpu__"
	templateVarMem              = "__crm_mem__"
	templateStorage             = "__crm_storage__"
	templateLimitVarCPU         = "__crm_limit_cpu__"
	templateLimitVarMem         = "__crm_limit_mem__"
	templateLimitStorage        = "__crm_limit_storage__"
	templateVarEnv              = "__crm_env__"
	templateVarEnvKey           = "__crm_env_key__"
	templateVarEnvValue         = "__crm_env_value__"
	templateVarPorts            = "__crm_ports__"
	templateVarPortsName        = "__crm_ports_name__"
	templateVarPortsContainer   = "__crm_ports_container__"
	templateVarPortsHost        = "__crm_ports_host__"
	templateVarPlatform         = "__crm_platform__"
	templateVarPlatformKey      = "__crm_platform_key__"
	templateVarCity             = "__crm_city__"
	templateVarCityKey          = "__crm_city_key__"
	templateVarVolumeMounts     = "__crm_volume_mounts__"
	templateVarVolumes          = "__crm_volumes__"
	templateVarVolumeMountsName = "__crm_volume_mounts_name__"
	templateVarVolumeMountsPath = "__crm_volume_mounts_path__"
	templateVarVolumeHostPath   = "__crm_volume_host_path__"
	templateVarRandPortNames    = "__crm_rand_port_names__"
	templateVarHostNetwork      = "__crm_host_network__"

	templateContentEnv = "" +
		"        - name: __crm_env_key__\n" +
		"          value: __crm_env_value__"

	templateContentPorts = "" +
		"        - name: __crm_ports_name__\n" +
		"          containerPort: __crm_ports_container__\n" +
		"          hostPort: __crm_ports_host__"

	templateContentVolumeMounts = "" +
		"        - mountPath: __crm_volume_mounts_path__\n" +
		"          name: __crm_volume_mounts_name__"

	templateContentVolumes = "" +
		"      - name: __crm_volume_mounts_name__\n" +
		"        hostPath:\n" +
		"          path: __crm_volume_host_path__\n" +
		"          type: DirectoryOrCreate"
)

// NewOperator get a new operator.
// TODO: For now, k8s operator do not support to deploy multi instances in one node(all pods with some host port).
//
//	So the request_cpu must big enough to occupy whole resource in one node. This should be solved later, and handle
//	the ports managements.
func NewOperator(conf *config.ContainerResourceConfig) (op.Operator, error) {
	data, err := os.ReadFile(conf.BcsAppTemplate)
	if err != nil {
		blog.Errorf("get new operator, read template file failed: %v", err)
		return nil, err
	}
	blog.Infof("crm: load bcs application template: \n%s", string(data))

	o := &operator{
		conf:               conf,
		templates:          string(data),
		clusterClientCache: make(map[string]*clusterClientSet),
		clusterCacheLock:   make(map[string]*sync.Mutex),
		disableWinHostNW:   conf.BcsDisableWinHostNW,
	}
	o.cityLabelKey = o.getCityLabelKey()
	o.platformLabelKey = o.getPlatformLabelKeyLabelKey()
	return o, nil
}

// k8s operator for bcs operations
type operator struct {
	conf      *config.ContainerResourceConfig
	templates string

	clusterClientCache map[string]*clusterClientSet
	cacheLock          sync.RWMutex
	clusterCacheLock   map[string]*sync.Mutex

	cityLabelKey     string
	platformLabelKey string
	disableWinHostNW bool
}

type clusterClientSet struct {
	clientSet   *kubernetes.Clientset
	timeoutTime time.Time
}

// GetResource get specific cluster's resources.
func (o *operator) GetResource(clusterID string) ([]*op.NodeInfo, error) {
	// BCS 联邦集群
	if o.conf.BcsClusterType == FederationCluster {
		return o.getFederationResource(clusterID)
	}

	// BCS or 原生集群
	return o.getResource(clusterID)
}

// GetServerStatus get the specific service(application and its taskgroup) status.
func (o *operator) GetServerStatus(clusterID, namespace, name string) (*op.ServiceInfo, error) {
	return o.getServerStatus(clusterID, namespace, name)
}

// LaunchServer launch a new service with given bcsLaunchParam.
func (o *operator) LaunchServer(clusterID string, param op.BcsLaunchParam) error {
	return o.launchServer(clusterID, param)
}

// ScaleServer scale worker instances of a existing service.
func (o *operator) ScaleServer(clusterID string, namespace, name string, instance int) error {
	return nil
}

// ReleaseServer release the specific service(application).
func (o *operator) ReleaseServer(clusterID, namespace, name string) error {
	return o.releaseServer(clusterID, namespace, name)
}

func (o *operator) getResource(clusterID string) ([]*op.NodeInfo, error) {
	blog.Debugf("k8s-operator: get resource for clusterID(%s)", clusterID)
	client, err := o.getClientSet(clusterID)
	if err != nil {
		blog.Errorf("k8s-operator: try to get resource from clusterID(%s) and get client set failed: %v",
			clusterID, err)
		return nil, err
	}
	nodeList, err := client.clientSet.CoreV1().Nodes().List(context.TODO(), metaV1.ListOptions{})
	if err != nil {
		blog.Errorf("k8s-operator: get node list from k8s failed clusterID(%s): %v", clusterID, err)
		return nil, err
	}

	fieldSelector, err := fields.ParseSelector(
		"status.phase!=" + string(coreV1.PodSucceeded) + ",status.phase!=" + string(coreV1.PodFailed))
	if err != nil {
		blog.Errorf("k8s-operator: generate field selector for k8s nodes failed: %v", err)
		return nil, err
	}
	nodeNonTerminatedPodsList, err := client.clientSet.CoreV1().Pods("").
		List(context.TODO(), metaV1.ListOptions{FieldSelector: fieldSelector.String()})
	if err != nil {
		blog.Errorf("k8s-operator: get pod list from k8s failed clusterID(%s): %v", clusterID, err)
		return nil, err
	}

	nodeInfoList := make([]*op.NodeInfo, 0, 1000)
	for _, node := range nodeList.Items {

		// get internal ip from status
		ip := ""
		for _, addr := range node.Status.Addresses {
			if addr.Type == coreV1.NodeInternalIP {
				ip = addr.Address
				break
			}
		}
		if ip == "" {
			blog.Errorf("k8s-operator: get node(%s) address internal ip empty, clusterID(%s)",
				node.Name, clusterID)
			continue
		}

		allocatedResource := getPodsTotalRequests(node.Name, nodeNonTerminatedPodsList)

		// get disable information from labels
		dl, _ := node.Labels[disableLabel]
		disabled := dl == "true"

		memTotal := float64(node.Status.Capacity.Memory().Value()) / 1024 / 1024
		cpuTotal := float64(node.Status.Capacity.Cpu().Value())
		memUsed := float64(allocatedResource.Memory().Value()) / 1024 / 1024
		cpuUsed := float64(allocatedResource.Cpu().Value())
		diskUsed := float64(allocatedResource.StorageEphemeral().Value())
		diskTotal := float64(node.Status.Capacity.StorageEphemeral().Value())
		for _, ist := range o.conf.InstanceType {
			if ist.Group == node.Labels[o.cityLabelKey] && ist.Platform == node.Labels[o.platformLabelKey] {
				if ist.CPUPerInstanceOffset > 0.0 || ist.MemPerInstanceOffset > 0.0 {
					//通过offset计算实际可用的instance数量，并矫正cpu和内存总量
					n := op.NodeInfo{
						MemTotal:  memTotal,
						CPUTotal:  cpuTotal,
						MemUsed:   memUsed,
						CPUUsed:   cpuUsed,
						DiskTotal: diskTotal,
						DiskUsed:  diskUsed,
					}
					availableNum := n.FigureAvailableInstanceFromFree(ist.CPUPerInstance-ist.CPUPerInstanceOffset, ist.MemPerInstance-ist.MemPerInstanceOffset, 1)
					cpuTotal = cpuUsed + float64(availableNum)*ist.CPUPerInstance
					memTotal = memUsed + float64(availableNum)*ist.MemPerInstance
				}
				break
			}
		}
		// use city-label-key value and platform-label-key to overwrite the city and platform
		node.Labels[op.AttributeKeyCity], _ = node.Labels[o.cityLabelKey]
		node.Labels[op.AttributeKeyPlatform], _ = node.Labels[o.platformLabelKey]
		nodeInfoList = append(nodeInfoList, &op.NodeInfo{
			IP:         ip,
			Hostname:   node.Name,
			DiskTotal:  diskTotal,
			MemTotal:   memTotal,
			CPUTotal:   cpuTotal,
			DiskUsed:   diskUsed,
			MemUsed:    memUsed,
			CPUUsed:    cpuUsed,
			Attributes: node.Labels,
			Disabled:   disabled,
		})
	}

	blog.Debugf("k8s-operator: success to get resource clusterID(%s)", clusterID)
	return nodeInfoList, nil
}

func (o *operator) getClient(timeoutSecond int) *httpclient.HTTPClient {
	client := httpclient.NewHTTPClient()
	client.SetTimeOut(time.Duration(timeoutSecond) * time.Second)
	return client
}

func (o *operator) request(method, uri string, requestHeader http.Header, data []byte) (raw []byte, err error) {
	var r *httpclient.HttpResponse

	client := o.getClient(reqTimeoutSecs)
	before := time.Now().Local()

	// add auth token in header
	header := http.Header{}
	if requestHeader != nil {
		for k := range requestHeader {
			header.Set(k, requestHeader.Get(k))
		}
	}
	header.Set("Authorization", fmt.Sprintf("Bearer %s", o.conf.BcsAPIToken))
	switch strings.ToUpper(method) {
	case "GET":
		if r, err = client.Get(uri, header, data); err != nil {
			return
		}
	case "POST":
		if r, err = client.Post(uri, header, data); err != nil {
			return
		}
	case "PUT":
		if r, err = client.Put(uri, header, data); err != nil {
			return
		}
	case "DELETE":
		if r, err = client.Delete(uri, header, data); err != nil {
			return
		}
	}
	raw = r.Reply

	now := time.Now().Local()
	if before.Add(reqSlowWarnSecs * time.Second).Before(now) {
		blog.Warnf("crm: operator request [%s] %s for too long: %s", method, uri, now.Sub(before).String())
	}

	if r.StatusCode != http.StatusOK {
		err = fmt.Errorf("crm: failed to request, http(%d)%s: %s", r.StatusCode, r.Status, uri)
		return
	}
	return
}

// FederationResourceParam define
type FederationResourceParam struct {
	Resources     ResRequests       `json:"resources"`
	ClusterID     string            `json:"clusterID"` //子集群ID，非联邦集群ID
	ClusterLabels map[string]string `json:"clusterLabels"`
	NodeSelector  map[string]string `json:"nodeSelector"`
}

// ResRequests define
type ResRequests struct {
	Requests ResRequest `json:"requests"`
}

// ResRequest define
type ResRequest struct {
	CPU    string `json:"cpu"`
	Memory string `json:"memory"`
}

// FederationData define
type FederationData struct {
	Total int `json:"total"`
}

// FederationResult define
type FederationResult struct {
	Code int            `json:"code"`
	Msg  string         `json:"msg"`
	Data FederationData `json:"data"`
}

func getCPUAndMemIst(ist config.InstanceType) (float64, float64) {
	varCPU := ist.CPUPerInstance
	varMem := ist.MemPerInstance
	if ist.CPUPerInstanceOffset > 0.0 && ist.CPUPerInstanceOffset < varCPU {
		varCPU = varCPU - ist.CPUPerInstanceOffset
	}
	if ist.MemPerInstanceOffset > 0.0 && ist.MemPerInstanceOffset < varMem {
		varMem = varMem - ist.MemPerInstanceOffset
	}
	return varCPU, varMem
}

func (o *operator) getFederationTotalNum(url string, ist config.InstanceType) (*FederationResult, error) {
	varCPU, varMem := getCPUAndMemIst(ist)
	param := &FederationResourceParam{
		Resources: ResRequests{
			Requests: ResRequest{
				CPU:    fmt.Sprintf("%f", varCPU),
				Memory: fmt.Sprintf("%fM", varMem),
			},
		},
		NodeSelector: map[string]string{
			o.platformLabelKey: ist.Platform,
			o.cityLabelKey:     ist.Group,
		},
	}
	var data []byte
	_ = codec.EncJSON(param, &data)
	// add auth token in header
	header := http.Header{}
	header.Set("Authorization", fmt.Sprintf("Bearer %s", o.conf.BcsAPIToken))
	res, err := o.request("POST", url, header, data)
	if err != nil {
		blog.Errorf("k8s operator: get federation resource param(%v), token(%v) failed: %v", param, header, err)
		return nil, err
	}

	result := &FederationResult{}
	if err = codec.DecJSON(res, result); err != nil {
		blog.Errorf("k8s operator: get federation decode url(%s) param(%v) token(%v) failed: %v", url, param, header, err)
		return nil, err
	}
	return result, nil
}

func (o *operator) getPodList(clusterID string) (*coreV1.PodList, error) {
	blog.Debugf("k8s-operator: begin to get federation podlist %s, %s", clusterID, o.conf.BcsNamespace)

	if o.conf.BcsNamespace == "" {
		return nil, fmt.Errorf("k8s-operator: get podlist failed clusterID(%s): namespace is nil", clusterID)
	}
	client, err := o.getClientSet(clusterID)
	if err != nil {
		blog.Errorf("k8s-operator: try to get podlist clusterID(%s) namespace(%s)"+
			"and get client set failed: %v", clusterID, o.conf.BcsNamespace, err)
		return nil, err
	}
	podList, err := client.clientSet.CoreV1().Pods(o.conf.BcsNamespace).List(context.TODO(), metaV1.ListOptions{})
	if err != nil {
		blog.Errorf("k8s-operator: get pod list from k8s failed clusterID(%s): %v", clusterID, err)
		return nil, err
	}
	return podList, nil
}

func (o *operator) getFederationResource(clusterID string) ([]*op.NodeInfo, error) {
	blog.Debugf("k8s-operator: begin to get federation resource %s, %s", clusterID, o.conf.BcsNamespace)
	nodeInfoList := make([]*op.NodeInfo, 0, 1000)
	if o.conf.BcsNamespace == "" {
		return nil, fmt.Errorf("crm: get federation resource request failed clusterID(%s): namespace is nil", clusterID)
	}
	url := fmt.Sprintf(bcsAPIFederatedURI, o.conf.BcsAPIPool.GetAddress(), clusterID, o.conf.BcsNamespace)
	podList, err := o.getPodList(clusterID)
	if err != nil {
		return nodeInfoList, fmt.Errorf("crm: get federation resource request failed clusterID(%s): %s", clusterID, err)
	}

	for _, ist := range o.conf.InstanceType {
		result, err := o.getFederationTotalNum(url, ist)
		if err != nil { //接口请求失败，直接返回错误
			err := fmt.Errorf("crm: get federation resource request failed url(%s) clusterID(%s) group(%s), platform(%s) : %v",
				url, clusterID, ist.Group, ist.Platform, err)
			return nodeInfoList, err
		}
		if result == nil { //无结果返回，直接返回错误
			err := fmt.Errorf("crm: get federation resource request failed url(%s) clusterID(%s) group(%s), platform(%s): result is nil",
				url, clusterID, ist.Group, ist.Platform)
			return nodeInfoList, err
		}
		if result.Code != 0 { //接口返回错误，直接返回错误
			err := fmt.Errorf("crm: get federation resource request failed url(%s) clusterID(%s) group(%s), platform(%s): (%v)%s",
				url, clusterID, ist.Group, ist.Platform, result.Code, result.Msg)
			return nodeInfoList, err
		}
		totalIst := float64(result.Data.Total)
		resourceUsed := make(coreV1.ResourceList)
		for _, pod := range podList.Items {
			if pod.Status.Phase == coreV1.PodSucceeded || pod.Status.Phase == coreV1.PodFailed {
				continue
			}
			if pod.Spec.NodeSelector != nil {
				if pod.Spec.NodeSelector[o.platformLabelKey] != ist.Platform ||
					pod.Spec.NodeSelector[o.cityLabelKey] != ist.Group {
					continue
				}
			}
			for podName, podLimitValue := range podLimits(&pod) {
				if value, ok := resourceUsed[podName]; !ok {
					resourceUsed[podName] = podLimitValue.DeepCopy()
				} else {
					value.Add(podLimitValue)
					resourceUsed[podName] = value
				}
			}
		}
		nodeInfoList = append(nodeInfoList, &op.NodeInfo{
			IP:       clusterID + "-" + o.conf.BcsNamespace + "-" + ist.Platform + "-" + ist.Group,
			Hostname: clusterID + "-" + o.conf.BcsNamespace + "-" + ist.Platform + "-" + ist.Group,
			DiskLeft: totalIst,
			//CPULeft:  totalIst * ist.CPUPerInstance,
			//MemLeft:  totalIst * ist.MemPerInstance,
			MemUsed:  float64(resourceUsed.Memory().Value()) / 1024 / 1024,
			CPUUsed:  float64(resourceUsed.Cpu().Value()),
			DiskUsed: float64(resourceUsed.StorageEphemeral().Value()),
			CPUTotal: float64(resourceUsed.Cpu().Value()) + totalIst*ist.CPUPerInstance,
			MemTotal: float64(resourceUsed.Memory().Value())/1024/1024 + totalIst*ist.MemPerInstance,
			Attributes: map[string]string{
				op.AttributeKeyPlatform: ist.Platform,
				op.AttributeKeyCity:     ist.Group,
			},
		})

	}
	blog.Debugf("k8s-operator: success to get federation resource clusterID(%s), ns(%s)", clusterID, o.conf.BcsNamespace)
	return nodeInfoList, nil
}

func (o *operator) getServerStatus(clusterID, namespace, name string) (*op.ServiceInfo, error) {
	info := &op.ServiceInfo{}

	if err := o.getDeployments(clusterID, namespace, name, info); err != nil {
		blog.Errorf("k8s-operator: get server status, get deployments clusterID(%s) namespace(%s) failed: %v",
			clusterID, namespace, err)
		return nil, err
	}

	if err := o.getPods(clusterID, namespace, name, info); err != nil {
		blog.Errorf("k8s-operator: get server status, get pods clusterID(%s) namespace(%s) failed: %v",
			clusterID, namespace, err)
		return nil, err
	}

	return info, nil
}

func (o *operator) getDeployments(clusterID, namespace, name string, info *op.ServiceInfo) error {
	client, err := o.getClientSet(clusterID)
	if err != nil {
		blog.Errorf("k8s-operator: try to get deployment clusterID(%s) namespace(%s) name(%s) "+
			"and get client set failed: %v", clusterID, namespace, name, err)
		return err
	}

	deploy, err := client.clientSet.AppsV1().Deployments(namespace).Get(context.TODO(), name, metaV1.GetOptions{})
	if err != nil {
		blog.Errorf("k8s-operator: get deployment clusterID(%s) namespace(%s) name(%s) failed: %v",
			clusterID, namespace, name, err)
		return err
	}

	info.Status = op.ServiceStatusRunning
	info.RequestInstances = int(deploy.Status.Replicas)
	if deploy.Status.UnavailableReplicas > 0 || deploy.Status.Replicas == 0 {
		info.Status = op.ServiceStatusStaging
	}

	blog.Debugf("k8s-operator: get deployment successfully, AppName(%s) NS(%s)",
		name, namespace)
	return nil
}

func (o *operator) getPods(clusterID, namespace, name string, info *op.ServiceInfo) error {
	client, err := o.getClientSet(clusterID)
	if err != nil {
		blog.Errorf("k8s-operator: try to get deployment clusterID(%s) namespace(%s) name(%s) "+
			"and get client set failed: %v", clusterID, namespace, name, err)
		return err
	}

	podList, err := client.clientSet.CoreV1().Pods(namespace).List(context.TODO(), metaV1.ListOptions{
		LabelSelector: fmt.Sprintf("%s=%s", appLabel, name),
	})
	if err != nil {
		blog.Errorf("k8s-operator: try to get podList clusterID(%s) namespace(%s) name(%s) failed: %v", clusterID, namespace, name, err)
	}
	availableEndpoint := make([]*op.Endpoint, 0, 100)
	for _, pod := range podList.Items {
		if pod.Status.Phase != coreV1.PodRunning {
			if info.Status != op.ServiceStatusStaging && pod.Status.Phase != coreV1.PodPending {
				blog.Warnf("k8s-operator: pod(%s) of %s in wrong status(%s)", pod.Name, name, pod.Status.Phase)
			}
			if (info.Status != op.ServiceStatusStaging) && (pod.Status.Phase == coreV1.PodPending) {
				blog.Warnf("k8s-operator: there is still a pod(%s) of %s in status(%s), "+
					"server status will be set to staging by force", pod.Name, name, pod.Status.Phase)
				info.Status = op.ServiceStatusStaging
			}
			continue
		}

		if len(pod.Status.ContainerStatuses) <= 0 || len(pod.Spec.Containers) <= 0 {
			blog.Warnf("k8s-operator: found exception pod of %s:[%+v]", name, pod)
			continue
		}

		ports := make(map[string]int)
		if len(pod.Spec.Containers[0].Ports) == 0 {
			blog.Warnf("k8s-operator: found empty port info in pod %s of %s:[%+v]", pod.Name, name, pod)
		}
		for _, port := range pod.Spec.Containers[0].Ports {
			ports[k8sPort2EnginePort(port.Name)] = int(port.HostPort)
		}

		availableEndpoint = append(availableEndpoint, &op.Endpoint{
			IP:    pod.Status.HostIP,
			Ports: ports,
			Name:  pod.Name,
		})
	}
	// if taskgroup are not all built, just means that the application is staging yet.
	if (info.RequestInstances > len(podList.Items)) && info.Status != op.ServiceStatusStaging {
		blog.Warnf("k8s-operator: found RequestInstances(%d) greater than pods num(%d) of %s in status %s", info.RequestInstances, len(podList.Items), name, info.Status)
		info.Status = op.ServiceStatusStaging
	}

	info.CurrentInstances = len(availableEndpoint)
	info.AvailableEndpoints = availableEndpoint
	return nil
}

func (o *operator) launchServer(clusterID string, param op.BcsLaunchParam) error {
	yamlData, err := o.getYAMLFromTemplate(param)
	if err != nil {
		blog.Errorf("k8s-operator: launch server for clusterID(%s) namespace(%s) name(%s) "+
			"get json from template failed: %v", clusterID, param.Namespace, param.Name, err)
		return err
	}

	blog.Infof("k8s-operator: launch deployment, clusterID(%s) namespace(%s) name(%s), "+
		"yaml:\n %s", clusterID, param.Namespace, param.Name, yamlData)
	client, err := o.getClientSet(clusterID)
	if err != nil {
		blog.Errorf("k8s-operator: try to launch server for clusterID(%s) namespace(%s) name(%s) "+
			"and get client set failed: %v", clusterID, param.Namespace, param.Name, err)
		return err
	}

	var deployment appsV1.Deployment
	if err = yaml.Unmarshal([]byte(yamlData), &deployment); err != nil {
		blog.Errorf("k8s-operator: create deployment namespace(%s) name(%s) from clusterID(%s), "+
			"decode from data failed: %v",
			param.Namespace, param.Name, clusterID, err)
		return err
	}

	if _, err = client.clientSet.AppsV1().Deployments(param.Namespace).
		Create(context.TODO(), &deployment, metaV1.CreateOptions{}); err != nil {
		blog.Errorf("k8s-operator: create deployment namespace(%s) name(%s) from clusterID(%s) failed: %v",
			param.Namespace, param.Name, clusterID, err)
		return err
	}

	blog.Infof("k8s-operator: success to create deployment namespace(%s) name(%s) from clusterID(%s)",
		param.Namespace, param.Name, clusterID)
	return nil
}

func (o *operator) scaleServer(clusterID, namespace, name string, instance int) error {
	return nil
}

func (o *operator) releaseServer(clusterID, namespace, name string) error {
	blog.Infof("k8s-operator: release server: clusterID(%s) namespace(%s) name(%s)",
		clusterID, namespace, name)
	client, err := o.getClientSet(clusterID)
	if err != nil {
		blog.Errorf("k8s-operator: try to release server for clusterID(%s) namespace(%s) name(%s) "+
			"and get client set failed: %v", clusterID, namespace, name, err)
		return err
	}

	var gracePeriodSeconds int64 = 0
	propagationPolicy := metaV1.DeletePropagationBackground
	if err = client.clientSet.AppsV1().Deployments(namespace).
		Delete(
			context.TODO(),
			name,
			metaV1.DeleteOptions{GracePeriodSeconds: &gracePeriodSeconds, PropagationPolicy: &propagationPolicy},
		); err != nil {
		if strings.Contains(err.Error(), "not found") {
			blog.Warnf("k8s-operator: release server clusterID(%s) namespace(%s) name(%s) not found, "+
				"regarded as released: %v", clusterID, namespace, name, err)
			return nil
		}

		blog.Errorf("k8s-operator: release server for clusterID(%s) namespace(%s) name(%s) failed: %v",
			clusterID, namespace, name, err)
		return err
	}
	blog.Infof("k8s-operator: success to release server: clusterID(%s) namespace(%s) name(%s)",
		clusterID, namespace, name)
	return nil
}

type portsMap struct {
	protocol string
	port     int
}

const (
	UbaPortName   = "uba-port"
	UbaPortNumber = 1345
	UbaProtocol   = "tcp"
)

func (o *operator) getYAMLFromTemplate(param op.BcsLaunchParam) (string, error) {
	// set platform
	platform := "linux"
	networkValue := ""
	if v, ok := param.AttributeCondition[op.AttributeKeyPlatform]; ok {
		switch v {
		case "windows", "WINDOWS", "win", "WIN":
			platform = "windows"
			if !o.disableWinHostNW {
				networkValue = "hostNetwork: true"
			}
		}
	}

	// add host port to env
	index := 0
	pm := make(map[string]portsMap)
	randPortsNames := make([]string, 0, 10)
	for port := range param.Ports {
		portNum := specificPort + index

		param.Env[envKeyHostPort+port] = fmt.Sprintf("%d", portNum)
		param.Env[envKeyRandPort+port] = fmt.Sprintf("%d", portNum)
		pm[port] = portsMap{
			protocol: param.Ports[port],
			port:     portNum,
		}
		index++

		randPortsNames = append(randPortsNames, enginePort2K8SPort(port))
	}

	// to support uba listen port
	if platform == "windows" {
		pm[UbaPortName] = portsMap{
			protocol: UbaProtocol,
			port:     UbaPortNumber,
		}

		randPortsNames = append(randPortsNames, UbaPortName)
	}

	data := o.templates
	data = strings.ReplaceAll(data, templateVarImage, param.Image)
	data = strings.ReplaceAll(data, templateVarName, param.Name)
	data = strings.ReplaceAll(data, templateVarNamespace, param.Namespace)
	data = strings.ReplaceAll(data, templateVarInstance, strconv.Itoa(param.Instance))
	data = strings.ReplaceAll(data, templateVarRandPortNames, strings.Join(randPortsNames, ","))
	data = insertYamlPorts(data, pm)
	data = insertYamlEnv(data, param.Env)
	data = insertYamlVolumes(data, param.Volumes)

	// handle host network settings for k8s-windows need it, but linux not.
	data = strings.ReplaceAll(data, templateVarHostNetwork, networkValue)
	data = strings.ReplaceAll(data, templateVarPlatform, platform)
	data = strings.ReplaceAll(data, templateVarPlatformKey, o.platformLabelKey)

	// set city
	if _, ok := param.AttributeCondition[op.AttributeKeyCity]; !ok {
		return "", fmt.Errorf("unknown city for yaml")
	}
	city := param.AttributeCondition[op.AttributeKeyCity]
	data = strings.ReplaceAll(data, templateVarCity, city)
	data = strings.ReplaceAll(data, templateVarCityKey, o.cityLabelKey)

	//set instance default value
	varCPU, varMem := getCPUAndMemIst(config.InstanceType{
		CPUPerInstance:       o.conf.BcsCPUPerInstance,
		MemPerInstance:       o.conf.BcsMemPerInstance,
		CPUPerInstanceOffset: o.conf.BcsCPUPerInstanceOffset,
		MemPerInstanceOffset: o.conf.BcsMemPerInstanceOffset,
	})
	varLimitCPU := o.conf.BcsCPUPerInstance
	varLimitMem := o.conf.BcsMemPerInstance
	if o.conf.BcsCPULimitPerInstance > 0.0 {
		varLimitCPU = o.conf.BcsCPULimitPerInstance
	}
	if o.conf.BcsMemLimitPerInstance > 0.0 {
		varLimitMem = o.conf.BcsMemLimitPerInstance
	}

	for _, istItem := range o.conf.InstanceType {
		if !param.CheckQueueKey(istItem) {
			continue
		}
		if istItem.CPUPerInstance > 0.0 {
			varLimitCPU = istItem.CPUPerInstance
		}
		if istItem.MemPerInstance > 0.0 {
			varLimitMem = istItem.MemPerInstance
		}
		varCPU, varMem = getCPUAndMemIst(istItem)
		if istItem.CPULimitPerInstance > 0.0 {
			varLimitCPU = istItem.CPULimitPerInstance
		}
		if istItem.MemLimitPerInstance > 0.0 {
			varLimitMem = istItem.MemLimitPerInstance
		}
		break
	}
	storageRequest := ""
	storageLimitRequest := ""
	if o.conf.BcsStoragePerInstance > 0.0 {
		storageRequest = fmt.Sprintf("ephemeral-storage: %.2fGi", o.conf.BcsStoragePerInstance)
		storageLimitRequest = storageRequest
	}
	if o.conf.BcsStorageLimitPerInstance > 0.0 {
		storageLimitRequest = fmt.Sprintf("ephemeral-storage: %.2fGi", o.conf.BcsStorageLimitPerInstance)
	}
	data = strings.ReplaceAll(data, templateVarCPU, fmt.Sprintf("%.2f", varCPU*1000))
	data = strings.ReplaceAll(data, templateVarMem, fmt.Sprintf("%.2f", varMem))
	data = strings.ReplaceAll(data, templateStorage, storageRequest)
	data = strings.ReplaceAll(data, templateLimitVarCPU, fmt.Sprintf("%.2f", varLimitCPU*1000))
	data = strings.ReplaceAll(data, templateLimitVarMem, fmt.Sprintf("%.2f", varLimitMem))
	data = strings.ReplaceAll(data, templateLimitStorage, storageLimitRequest)
	return data, nil
}

func (o *operator) getClientSet(clusterID string) (*clusterClientSet, error) {
	// check if the client-set of this clusterID exists
	cs, ok := o.getClientSetFromCache(clusterID)
	if ok {
		return cs, nil
	}

	// make sure the cluster-cache-lock exist for this clusterID
	o.cacheLock.Lock()
	if _, ok = o.clusterCacheLock[clusterID]; !ok {
		o.clusterCacheLock[clusterID] = new(sync.Mutex)
	}
	cacheLock := o.clusterCacheLock[clusterID]
	o.cacheLock.Unlock()

	// lock cluster-cache-lock of this clusterID, and then check client-set again
	// else go generate new client.
	cacheLock.Lock()
	defer cacheLock.Unlock()
	cs, ok = o.getClientSetFromCache(clusterID)

	if ok {
		return cs, nil
	}
	return o.generateClient(clusterID)
}

func (o *operator) getClientSetFromCache(clusterID string) (*clusterClientSet, bool) {
	o.cacheLock.RLock()

	defer o.cacheLock.RUnlock()
	cs, ok := o.clusterClientCache[clusterID]

	if ok && cs.timeoutTime.Before(time.Now().Local()) {
		blog.Debugf("k8s-operator: the client from cache is out of date since(%s), should be regenerated",
			cs.timeoutTime.String())
		return nil, false
	}
	return cs, ok
}

func (o *operator) generateClient(clusterID string) (*clusterClientSet, error) {
	// 通过 crm_kubeconfig_path 配置原生 k8s 集群
	if o.conf.KubeConfigPath != "" {
		return o.generateNativeClient(clusterID, o.conf.KubeConfigPath)
	}

	return o.generateBCSClient(clusterID)
}

// generateNativeClient native cluster
func (o *operator) generateNativeClient(clusterID, kubeconfigPath string) (*clusterClientSet, error) {
	c, err := clientcmd.BuildConfigFromFlags("", kubeconfigPath)
	if err != nil {
		blog.Errorf("k8s-operator: get client set(%s), create new native client set, build config failed: %v", clusterID, err)
		return nil, err
	}

	// kubeConfig 配置优化, TLS certificate 等需要在 kubeconfig 配置
	c.QPS = 1e6
	c.Burst = 1e6

	clientSet, err := kubernetes.NewForConfig(c)
	if err != nil {
		blog.Errorf("k8s-operator: get client set(%s), create new native client set failed: %v", clusterID, err)
		return nil, err
	}

	cs := &clusterClientSet{
		clientSet:   clientSet,
		timeoutTime: time.Now().Local().Add(1 * time.Minute),
	}
	o.cacheLock.Lock()
	o.clusterClientCache[clusterID] = cs
	o.cacheLock.Unlock()

	blog.Infof("k8s-operator: get client set, create new native client set for cluster(%s), config host: %s", clusterID, c.Host)

	return cs, nil
}

// generateBCSClient bcs 客户端
func (o *operator) generateBCSClient(clusterID string) (*clusterClientSet, error) {
	address := o.conf.BcsAPIPool.GetAddress()
	var host string
	if o.conf.EnableBCSApiGw {
		host = fmt.Sprintf(bcsAPIGWK8SBaseURI, address, clusterID)
	} else {
		host = fmt.Sprintf(bcsAPIK8SBaseURI, address, clusterID)
	}

	blog.Infof("k8s-operator: try generate bcs client with host(%s) token(%s)", host, o.conf.BcsAPIToken)
	// get client set by real api-server address
	c := &rest.Config{
		Host:        host,
		BearerToken: o.conf.BcsAPIToken,
		QPS:         1e6,
		Burst:       1e6,
		Transport: &http.Transport{
			TLSHandshakeTimeout: 5 * time.Second,
			DialContext: (&net.Dialer{
				Timeout:   5 * time.Second,
				KeepAlive: 30 * time.Second,
			}).DialContext,
			ResponseHeaderTimeout: 30 * time.Second,
			TLSClientConfig:       &tls.Config{InsecureSkipVerify: true},
		},
	}

	blog.Infof("k8s-operator: get client set, create new bcs client set for cluster(%s), config: %v",
		clusterID, c)
	clientSet, err := kubernetes.NewForConfig(c)
	if err != nil {
		blog.Errorf("k8s-operator: get client set(%s), create new bcs client set failed: %v", clusterID, err)
		return nil, err
	}

	cs := &clusterClientSet{
		clientSet:   clientSet,
		timeoutTime: time.Now().Local().Add(1 * time.Minute),
	}
	o.cacheLock.Lock()
	o.clusterClientCache[clusterID] = cs
	o.cacheLock.Unlock()
	return cs, nil
}

func (o *operator) getCityLabelKey() string {
	if o.conf.BcsGroupLabelKey != "" {
		return o.conf.BcsGroupLabelKey
	}

	return op.AttributeKeyCity
}

func (o *operator) getPlatformLabelKeyLabelKey() string {
	if o.conf.BcsPlatformLabelKey != "" {
		return o.conf.BcsPlatformLabelKey
	}

	return "kubernetes.io/os"
}

func insertYamlPorts(data string, ports map[string]portsMap) string {
	portsYaml := ""
	index := 0

	for name, port := range ports {
		// for k8s storage rule.
		portName := enginePort2K8SPort(name)

		content := templateContentPorts
		content = strings.ReplaceAll(content, templateVarPortsName, portName)
		content = strings.ReplaceAll(content, templateVarPortsContainer, fmt.Sprintf("%d", port.port))
		content = strings.ReplaceAll(content, templateVarPortsHost, fmt.Sprintf("%d", port.port))

		portsYaml += "\n" + content

		index++
	}

	data = strings.ReplaceAll(data, templateVarPorts, portsYaml)
	return data
}

func insertYamlEnv(data string, env map[string]string) string {
	envYaml := ""

	for k, v := range env {
		content := templateContentEnv
		content = strings.ReplaceAll(content, templateVarEnvKey, k)
		content = strings.ReplaceAll(content, templateVarEnvValue, v)
		envYaml += "\n" + content
	}

	return strings.ReplaceAll(data, templateVarEnv, envYaml)
}

func insertYamlVolumes(data string, volumes map[string]op.BcsVolume) string {
	volumeMountsYaml := ""

	for k, v := range volumes {
		content := templateContentVolumeMounts
		content = strings.ReplaceAll(content, templateVarVolumeMountsPath, v.ContainerDir)
		content = strings.ReplaceAll(content, templateVarVolumeMountsName, k)
		volumeMountsYaml += "\n" + content
	}

	volumesYaml := ""

	for k, v := range volumes {
		content := templateContentVolumes
		content = strings.ReplaceAll(content, templateVarVolumeMountsName, k)
		content = strings.ReplaceAll(content, templateVarVolumeHostPath, v.HostDir)
		volumesYaml += "\n" + content
	}

	data = strings.ReplaceAll(data, templateVarVolumeMounts, volumeMountsYaml)
	data = strings.ReplaceAll(data, templateVarVolumes, volumesYaml)
	return data
}

func enginePort2K8SPort(name string) string {
	return strings.ReplaceAll(strings.ToLower(name), "_", "-")
}

func k8sPort2EnginePort(name string) string {
	return strings.ReplaceAll(strings.ToUpper(name), "-", "_")
}

func getPodsTotalRequests(nodeName string, podList *coreV1.PodList) coreV1.ResourceList {
	requests := make(coreV1.ResourceList)

	for _, pod := range podList.Items {
		if pod.Spec.NodeName != nodeName {
			continue
		}

		podRequests := podRequests(&pod)

		for podName, podRequestValue := range podRequests {
			if value, ok := requests[podName]; !ok {
				requests[podName] = podRequestValue.DeepCopy()
			} else {
				value.Add(podRequestValue)
				requests[podName] = value
			}
		}
	}

	return requests
}

func podRequests(pod *coreV1.Pod) coreV1.ResourceList {
	requests := coreV1.ResourceList{}
	for _, container := range pod.Spec.Containers {
		addResourceList(requests, container.Resources.Requests)
	}
	// init containers define the minimum of any resource
	for _, container := range pod.Spec.InitContainers {
		maxResourceList(requests, container.Resources.Requests)
	}

	return requests
}

func podLimits(pod *coreV1.Pod) coreV1.ResourceList {
	limits := coreV1.ResourceList{}
	for _, container := range pod.Spec.Containers {
		addResourceList(limits, container.Resources.Limits)
	}
	// init containers define the minimum of any resource
	for _, container := range pod.Spec.InitContainers {
		maxResourceList(limits, container.Resources.Limits)
	}

	return limits
}

// addResourceList adds the resources in newList to list
func addResourceList(list, new coreV1.ResourceList) {
	for name, quantity := range new {
		if value, ok := list[name]; !ok {
			list[name] = quantity.DeepCopy()
		} else {
			value.Add(quantity)
			list[name] = value
		}
	}
}

// maxResourceList sets list to the greater of list/newList for every resource
// either list
func maxResourceList(list, new coreV1.ResourceList) {
	for name, quantity := range new {
		if value, ok := list[name]; !ok {
			list[name] = quantity.DeepCopy()
			continue
		} else {
			if quantity.Cmp(value) > 0 {
				list[name] = quantity.DeepCopy()
			}
		}
	}
}

func getBcsK8SBaseURL() string {
	if len(EnableBCSApiGw) > 0 {
		return bcsAPIGWK8SBaseURI
	}

	return bcsAPIK8SBaseURI
}
