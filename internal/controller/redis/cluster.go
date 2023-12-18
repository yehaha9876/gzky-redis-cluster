package controller

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"strconv"
	"strings"

	redisv9 "github.com/redis/go-redis/v9"
	redisv1 "gitlab.gzky.com/sean.liu/gzky-redis-operator/api/v1"
	corev1 "k8s.io/api/core/v1"
)

const (
	DefaultRedisPort = "6379"
)

type ClusterRedis struct {
	RedisNodes     []RedisNode
	ReqMasterCount int
	ReqSlaveCount  int
	Status         string
}

type RedisNode struct {
	Id          string
	Host        string
	Port        string
	Role        string
	Slots       Slots
	OperateSlot Slots
	MasterId    string

	Status string
	Client *redisv9.Client
	Pod    corev1.Pod
}

type Slots struct {
	SlotStr       string
	SlotSlice     []interface{}
	SlotImporting []interface{}
	SlotMigrating []interface{}
}

func SlotsStringToInterface(slots string) (interface{}, error) {
	if strings.Contains(slots, "-") {
		bef, aft, found := strings.Cut(slots, "-")
		if found {
			befI, err := strconv.Atoi(bef)
			aftI, err1 := strconv.Atoi(aft)
			if err != nil || err1 != nil {
				fmt.Println(err, err1)
				return nil, errors.New("Can't convert slot string string to int error")
			}
			rangeSlice = []int{befI, aftI}
			return rangeSlice, nil
		}
	} else {
		if slot, err := strconv.Atoi(slots); err != nil {
			return slot, nil
		}
	}
	return nil, errors.New("Can't convert slot string")
}

func NewSlots(slotStr string) Slots {
	slots := Slots{SlotStr: slotStr}

	slot_slice := strings.Split(slotStr, " ")
	for _, s := range slot_slice {
		if strings.Contains(s, "[") {
			s, _ = strings.CutPrefix(s, "[")
			if strings.Contains(s, "->-") {
				bef, _, _ := strings.Cut(s, "->-")
				if slotInterf, err := SlotsStringToInterface(bef); err == nil {
					slots.SlotMigrating = append(slots.SlotMigrating, slotInterf)
				}
			} else if strings.Contains(s, "-<-") {
				bef, _, _ := strings.Cut(s, "-<-")
				if slotInterf, err := SlotsStringToInterface(bef); err == nil {
					slots.SlotImporting = append(slots.SlotImporting, slotInterf)
				}
			} else {
				fmt.Println("Unexpect slot format")
			}
		} else {
			if slotInterf, err := SlotsStringToInterface(bef); err == nil {
				slots.SlotSlice = append(slots.SlotSlice, slotInterf)
			}
		}
	}
	return slots
}

func (r *ClusterRedis) InitCluster(podList []corev1.Pod, redisCluster redisv1.RedisCluster, redisConfig map[string]string) {
	r.ReqMasterCount = int(redisCluster.Spec.MasterNum)
	r.ReqSlaveCount = int(redisCluster.Spec.MasterNum * redisCluster.Spec.SlaveNumEach)

	for _, pod := range podList {
		redisNode := RedisNode{}
		redisNode.Pod = pod

		if pod.Status.PodIP != "" {
			redisNode.Host = pod.Status.PodIP
		}

		for _, container := range pod.Spec.Containers {
			for _, port := range container.Ports {
				if port.Name == "redis" {
					redisNode.Port = fmt.Sprintf("%d", port.ContainerPort)
				}
			}
		}
		if redisNode.Port == "" {
			redisNode.Port = DefaultRedisPort
		}
		r.RedisNodes = append(r.RedisNodes, redisNode)
	}
	r.InitClusterStatus()
}

func (r *ClusterRedis) InitClusterStatus() {
	for i := range r.RedisNodes {
		redisNode := &r.RedisNodes[i]
		client := redisv9.NewClient(&redisv9.Options{
			Addr: redisNode.Host + ":" + redisNode.Port,
			//Password: redisConfig["requirepass"],
			DB: 0,
		})
		redisNode.Client = client

		clusterInfo, err := client.ClusterInfo(context.Background()).Result()
		if err != nil {
			fmt.Println("CanNotConnect")
			redisNode.Status = "CanNotConnect"
			continue
		}
		fmt.Println("CLuster info:", clusterInfo)

		clusterInfoMap := ConverToMap(clusterInfo, ":")
		if clusterInfoMap["cluster_size"] == "0" && clusterInfoMap["cluster_current_epoch"] == "0" {
			fmt.Println(redisNode.Host, "not init")
			redisNode.Status = "NotInit"
			continue
		}

		if clusterInfoMap["cluster_state"] != "ok" {
			fmt.Println(redisNode.Host, "cluster Faild")
			r.Status = "Faild"
		}

		clusterNodes, err := client.ClusterNodes(context.Background()).Result()
		if err != nil {
			fmt.Println("CanNotConnect")
			redisNode.Status = "CanNotConnect"
			continue
		}
		converToRedisNode(clusterNodes, redisNode)
		fmt.Printf("redis nodes: %+v \n", redisNode)

		if redisNode.Status != "" && redisNode.Status != "ok" {
			r.Status = "Faild"
		} else {
			redisNode.Status = "ok"
		}
	}
}

func (r *ClusterRedis) OperateIfNeeded() {
	r.InitClusterOrAddNode()
}

func (r *ClusterRedis) InitClusterOrAddNode() bool {
	fmt.Println("start init cluster")
	if len(r.RedisNodes) < 1 {
		return false
	}

	okMasterRedisNode := make([]RedisNode, 0)
	okSlaveRedisNode := make([]RedisNode, 0)
	initRedisNode := make([]RedisNode, 0)

	for i := range r.RedisNodes {
		redisNode := &r.RedisNodes[i]
		if redisNode.Status == "NotInit" {
			initRedisNode = append(initRedisNode, *redisNode)
			continue
		}

		if redisNode.Status == "ok" {
			if redisNode.Role == "master" {
				okMasterRedisNode = append(okMasterRedisNode, *redisNode)
			} else if redisNode.Role == "slave" {
				okSlaveRedisNode = append(okSlaveRedisNode, *redisNode)
			}
		}
	}

	fmt.Println("init node", initRedisNode)

	if len(r.RedisNodes) == len(okSlaveRedisNode)+len(okMasterRedisNode) {
		return true
	}

	if len(initRedisNode) == 0 {
		return true
	}

	operatorNode := initRedisNode[0]
	if len(okMasterRedisNode) > 0 {
		operatorNode = okMasterRedisNode[0]
	}
	if len(initRedisNode) == r.ReqMasterCount+r.ReqSlaveCount {
		slotRangeSlice := createSlotRangeSlice(16384, r.ReqMasterCount)
	}
}

func addNode(operatorNode, addNodes) {
	for _, redisNode := range addNodes {
		fmt.Println("do meet", redisNode.Host)
		operatorNode.Client.ClusterMeet(context.Background(), redisNode.Host, redisNode.Port).Result()

		if redisNode.Role == "master" {
			operateSlot := redisNode.OperateSlot
			for _, slotRange := range operateSlot.SlotSlice {
				fmt.Println("add slot", slotRange)
				switch slotRange.(type) {
				case int:
					redisNode.Client.ClusterAddSlotsRange(context.Background(), slotRange[0], slotRange[1]).Result()
				case []int:
					redisNode.Client.ClusterAddSlots(context.Background(), slotRange).Result()
				}
			}

			for _, slotRange := range operateSlot.SlotImporting {
				switch slotRange.(type) {
				case int:
					redisNode.Client.ClusterAddSlotsRange(context.Background(), slotRange[0], slotRange[1]).Result()
				case []int:
					redisNode.Client.ClusterAddSlots(context.Background(), slotRange).Result()
				}
			}

			for _, slotRange := range operateSlot.SlotMigrating {
				switch slotRange.(type) {
				case int:
					redisNode.Client.ClusterAddSlotsRange(context.Background(), slotRange[0], slotRange[1]).Result()
				case []int:
					redisNode.Client.ClusterAddSlots(context.Background(), slotRange).Result()
				}
			}
		} else {
			fmt.Println("add replicate", slotRange)
			redisNode.Client.ClusterReplicate(context.Background(), redisNode.MasterId).Result()
		}
	}
}

func converToRedisNode(content string, redisNode *RedisNode) {
	if content == "" {
		return
	}

	for _, line := range strings.Split(content, "\n") {
		if strings.Contains(line, "self") {
			arr := strings.SplitN(line, " ", 9)
			if len(arr) < 9 {
				continue
			}

			redisNode.Id = strings.TrimSpace(arr[0])
			roleString := strings.TrimSpace(arr[2])
			if roleArr := strings.Split(roleString, ","); len(roleArr) == 2 {
				redisNode.Role = roleArr[1]
			}
			redisNode.Slots = NewSlots(arr[8])
		}
	}
}

func ConverToMap(content string, split string) map[string]string {
	contentMap := map[string]string{}
	if split == "" {
		split = ":"
	}
	re := regexp.MustCompile(split)

	for _, line := range strings.Split(content, "\n") {
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		arr := re.Split(line, 2)
		if len(arr) != 2 {
			continue
		}

		key, val := strings.TrimSpace(arr[0]), strings.TrimSpace(arr[1])
		contentMap[key] = val
	}
	return contentMap
}

func createSlotRangeSlice(slotNum, nodeCount int) []interface{} {
	step, lf := slotNum/nodeCount, slotNum%nodeCount
	slotSlices := make([]interface{}, 0)

	start := 0
	for i := 0; i < nodeCount; i++ {
		slots := make([]int, 2)
		slots[0] = start
		start = start + step
		if i >= lf {
			start--
		}
		slots[1] = start
		if slots[1] == slots[0] {
			slotSlices = append(slotSlices, start)
		} else {
			slotSlices = append(slotSlices, slots)
		}

		start++
	}
	return slotSlices
}
