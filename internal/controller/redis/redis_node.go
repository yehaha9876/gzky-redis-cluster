package redis

import (
	"regexp"
	"strings"

	redisv9 "github.com/redis/go-redis/v9"
)

type RedisNode struct {
	Id       string
	Host     string
	Port     string
	Role     string
	MasterId string
	Slots    Slots

	Client *redisv9.Client
	Status string
}

func NewRedisNode(host string, port string) {

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
