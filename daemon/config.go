package daemon

import (
	"context"

	"github.com/AliyunContainerService/terway/pkg/aliyun/instance"
	"github.com/AliyunContainerService/terway/pkg/k8s"
	"github.com/AliyunContainerService/terway/pkg/utils"
	"github.com/AliyunContainerService/terway/types"
	"github.com/AliyunContainerService/terway/types/daemon"
)

// getDynamicConfig returns (config, label, error) specified in node
// ("", "", nil) for no dynamic config for this node
func getDynamicConfig(ctx context.Context, k8s k8s.Kubernetes) (string, string, error) {
	label := k8s.GetNodeDynamicConfigLabel()
	if label == "" {
		return "", "", nil
	}

	cfg, err := k8s.GetDynamicConfigWithName(ctx, label)

	return cfg, label, err
}

// the actual size for pool is minIdle and maxIdle
func getPoolConfig(cfg *daemon.Config, daemonMode string, limit *instance.Limits) (*types.PoolConfig, error) {

	poolConfig := &types.PoolConfig{
		SecurityGroupIDs:          cfg.GetSecurityGroups(),
		VSwitchSelectionPolicy:    cfg.VSwitchSelectionPolicy,
		DisableSecurityGroupCheck: cfg.DisableSecurityGroupCheck,
		BatchSize:                 10,
	}

	if cfg.ENITags == nil {
		cfg.ENITags = make(map[string]string)
	}
	cfg.ENITags[types.NetworkInterfaceTagCreatorKey] = types.NetworkInterfaceTagCreatorValue

	poolConfig.ENITags = cfg.ENITags

	capacity := 0
	maxENI := 0
	maxMemberENI := 0

	switch daemonMode {
	case daemon.ModeVPC, daemon.ModeENIOnly:
		maxENI = limit.Adapters
		maxENI = int(float64(maxENI)*cfg.EniCapRatio) + cfg.EniCapShift - 1

		// set max eni node can use
		if cfg.MaxENI > 0 && cfg.MaxENI < maxENI {
			maxENI = cfg.MaxENI
		}

		capacity = maxENI

		if cfg.MaxPoolSize > maxENI {
			poolConfig.MaxPoolSize = maxENI
		} else {
			poolConfig.MaxPoolSize = cfg.MaxPoolSize
		}

		poolConfig.MinPoolSize = cfg.MinPoolSize

		if cfg.MinENI > 0 {
			poolConfig.MinPoolSize = cfg.MinENI
		}

		if poolConfig.MinPoolSize > poolConfig.MaxPoolSize {
			poolConfig.MinPoolSize = poolConfig.MaxPoolSize
		}

		maxMemberENI = limit.MemberAdapterLimit
		if cfg.ENICapPolicy == daemon.ENICapPolicyPreferTrunk {
			maxMemberENI = limit.MaxMemberAdapterLimit
		}

		poolConfig.MaxIPPerENI = 1
		if cfg.EnableERDMA {
			poolConfig.ERdmaCapacity = limit.ERdmaAdapters
		}
	case daemon.ModeENIMultiIP:
		maxENI = limit.Adapters
		maxENI = int(float64(maxENI)*cfg.EniCapRatio) + cfg.EniCapShift - 1

		// set max eni node can use
		if cfg.MaxENI > 0 && cfg.MaxENI < maxENI {
			maxENI = cfg.MaxENI
		}

		ipPerENI := limit.IPv4PerAdapter
		if utils.IsWindowsOS() {
			// NB(thxCode): don't assign the primary IP of one assistant eni.
			ipPerENI--
		}

		capacity = maxENI * ipPerENI
		if cfg.MaxPoolSize > capacity {
			poolConfig.MaxPoolSize = capacity
		} else {
			poolConfig.MaxPoolSize = cfg.MaxPoolSize
		}

		poolConfig.MinPoolSize = cfg.MinPoolSize

		if cfg.MinENI > 0 {
			poolConfig.MinPoolSize = cfg.MinENI * ipPerENI
		}
		if poolConfig.MinPoolSize > poolConfig.MaxPoolSize {
			poolConfig.MinPoolSize = poolConfig.MaxPoolSize
		}

		maxMemberENI = limit.MemberAdapterLimit

		poolConfig.MaxIPPerENI = ipPerENI

		if cfg.EnableERDMA {
			poolConfig.ERdmaCapacity = limit.ERdmaAdapters * limit.IPv4PerAdapter
		}
	}

	poolConfig.ENITags = cfg.ENITags

	requireMeta := true
	if cfg.IPAMType == types.IPAMTypeCRD {
		poolConfig.MaxPoolSize = 0
		poolConfig.MinPoolSize = 0

		if cfg.DisableDevicePlugin {
			requireMeta = false
		}
	}

	if requireMeta {
		ins := instance.GetInstanceMeta()
		zone := ins.ZoneID
		if cfg.VSwitches != nil {
			zoneVswitchs, ok := cfg.VSwitches[zone]
			if ok && len(zoneVswitchs) > 0 {
				poolConfig.VSwitchOptions = cfg.VSwitches[zone]
			}
		}
		if len(poolConfig.VSwitchOptions) == 0 {
			poolConfig.VSwitchOptions = []string{ins.VSwitchID}
		}
		poolConfig.ZoneID = zone
		poolConfig.InstanceID = ins.InstanceID
	}

	poolConfig.Capacity = capacity
	poolConfig.MaxENI = maxENI
	poolConfig.MaxMemberENI = maxMemberENI

	return poolConfig, nil
}
