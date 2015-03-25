package meta

import (
	"fmt"
	"log"
	"strconv"
	"strings"

	"launchpad.net/gozk"
)

func (m *Meta) leaders() (string, string, <-chan zookeeper.Event, error) {
	zkPath := m.ccDirPath
	zconn := m.zconn

	children, stat, watcher, err := zconn.ChildrenW(zkPath)
	if err != nil {
		return "", "", watcher, err
	}
	if stat.NumChildren() == 0 {
		return "", "", watcher, fmt.Errorf("meta: no node in controller leader directory")
	}

	needRejoin := true
	clusterMinSeq := -1
	clusterLeader := ""
	regionMinSeq := -1
	regionLeader := ""

	for _, child := range children {
		xs := strings.Split(child, "_")
		seq, _ := strconv.Atoi(xs[2])
		region := xs[1]
		// Cluster Leader
		if clusterMinSeq < 0 {
			clusterMinSeq = seq
			clusterLeader = child
		}
		if seq < clusterMinSeq {
			clusterMinSeq = seq
			clusterLeader = child
		}
		// Region Leader
		if m.localRegion == region {
			if regionMinSeq < 0 {
				regionMinSeq = seq
				regionLeader = child
			}
			if seq < regionMinSeq {
				regionMinSeq = seq
				regionLeader = child
			}
		}
		// Rejoin
		if m.selfZNodeName == child {
			needRejoin = false
		}
	}

	if needRejoin {
		err := m.RegisterLocalController()
		if err != nil {
			return "", "", watcher, err
		}
	}

	return clusterLeader, regionLeader, watcher, nil
}

func (m *Meta) handleClusterLeaderConfigChanged(znode string, watch <-chan zookeeper.Event) {
	for {
		event := <-watch
		if event.Type == zookeeper.EVENT_CHANGED {
			if m.clusterLeaderZNodeName != znode {
				log.Println("meta: region leader has changed")
				break
			}
			c, w, err := m.FetchControllerConfig(znode)
			if err == nil {
				m.clusterLeaderConfig = c
				log.Println("meta: cluster leader config changed.")
			} else {
				log.Printf("meta: fetch controller config failed, %v", err)
			}
			watch = w
		} else {
			log.Printf("meta: unexpected event coming, %v", event)
			break
		}
	}
}

func (m *Meta) handleRegionLeaderConfigChanged(znode string, watch <-chan zookeeper.Event) {
	for {
		event := <-watch
		if event.Type == zookeeper.EVENT_CHANGED {
			if m.regionLeaderZNodeName != znode {
				log.Println("meta: region leader has changed")
				break
			}
			c, w, err := m.FetchControllerConfig(znode)
			if err == nil {
				m.regionLeaderConfig = c
				log.Println("meta: region leader config changed.")
			} else {
				log.Printf("meta: fetch controller config failed, %v", err)
			}
			watch = w
		} else {
			log.Printf("meta: unexpected event coming, %v", event)
			break
		}
	}
}

func (m *Meta) ElectLeader() (<-chan zookeeper.Event, error) {
	clusterLeader, regionLeader, watcher, err := m.leaders()
	if err != nil {
		return watcher, err
	}
	if clusterLeader == "" || regionLeader == "" {
		return watcher, fmt.Errorf("meta: get leaders failed.")
	}

	log.Println("leader:", clusterLeader, regionLeader)

	if m.clusterLeaderZNodeName != clusterLeader {
		// 获取ClusterLeader配置
		c, w, err := m.FetchControllerConfig(clusterLeader)
		if err != nil {
			return watcher, err
		}
		m.clusterLeaderConfig = c
		m.clusterLeaderZNodeName = clusterLeader
		go m.handleClusterLeaderConfigChanged(clusterLeader, w)
	}

	if m.regionLeaderZNodeName != regionLeader {
		c, w, err := m.FetchControllerConfig(regionLeader)
		if err != nil {
			return watcher, err
		}
		m.regionLeaderConfig = c
		m.regionLeaderZNodeName = regionLeader
		go m.handleRegionLeaderConfigChanged(regionLeader, w)
	}
	return watcher, nil
}