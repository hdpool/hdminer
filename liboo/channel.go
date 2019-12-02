// channel.go 中转层，框架层与底层交互的管道，负责连接池管理、打包发送、回包转发
package oo

import (
	"errors"
	"sync"
	"sync/atomic"
)

type ChannelManager struct {
	rpcSequence uint64
	channelMap  sync.Map
	chlen       uint64
}

type Channel struct {
	Data     interface{}
	sequence uint64
	respChan chan interface{}
	closed   chan struct{}
	Chmgr    *ChannelManager
}

func (ch *Channel) GetSeq() uint64 {
	return ch.sequence
}

func (ch *Channel) Close() {
	ch.Chmgr.DelChannel(ch)
}

func (ch *Channel) IsClosed() <-chan struct{} {
	return ch.closed
}

func (ch *Channel) PushMsg(m interface{}) {
	defer func() {
		if errs := recover(); errs != nil {
			LogW("Failed recover: %v", errs)
		}
	}()
	ch.respChan <- m
}

func (ch *Channel) RecvChan() <-chan interface{} {
	return ch.respChan
}

func (cm *ChannelManager) NewChannel() *Channel {
	ch := &Channel{respChan: make(chan interface{}, cm.chlen), closed: make(chan struct{})}
	ch.sequence = atomic.AddUint64(&cm.rpcSequence, 1)
	ch.Chmgr = cm
	cm.channelMap.Store(ch.sequence, ch)
	return ch
}

func (cm *ChannelManager) DelChannel(ch *Channel) {
	cm.channelMap.Delete(ch.sequence)
	close(ch.closed)
}

func (cm *ChannelManager) GetChannel(seq uint64) (*Channel, error) {
	if v, ok := cm.channelMap.Load(seq); ok {
		ch, _ := v.(*Channel)
		return ch, nil
	}

	return nil, errors.New("not found")
}

func (cm *ChannelManager) PushChannelMsg(seq uint64, m interface{}) error {
	if nil == cm {
		return errors.New("no manager")
	}
	if v, ok := cm.channelMap.Load(seq); ok {
		ch, _ := v.(*Channel)
		select {
		case ch.respChan <- m:
			return nil
		default:
			return errors.New("chan full")
		}
	} else {
		return errors.New("not found")
	}
}

func (cm *ChannelManager) PushAllChannelMsg(m interface{}, fn func(Data interface{}) bool) error {
	if nil == cm {
		return errors.New("no manager")
	}
	cm.channelMap.Range(func(key, value interface{}) bool {
		ch, _ := value.(*Channel)
		if fn == nil || fn(ch.Data) {
			ch.PushMsg(m)
		}

		return true
	})
	return nil
}

func (cm *ChannelManager) PushOneChannelMsg(m interface{}, fn func(Data interface{}) bool) error {
	if nil == cm {
		return errors.New("no manager")
	}
	cm.channelMap.Range(func(key, value interface{}) bool {
		ch, _ := value.(*Channel)
		if fn(ch.Data) {
			ch.PushMsg(m)
			return false
		}

		return true
	})
	return nil
}

func NewChannelManager(chlen uint64) *ChannelManager {
	cm := &ChannelManager{1, sync.Map{}, chlen}
	return cm
}
