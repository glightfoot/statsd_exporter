// Copyright 2013 The Prometheus Authors
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
// http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package mapper

import (
	"sync"

	"github.com/prometheus/client_golang/prometheus"
)

var cacheMutex = &sync.RWMutex{}

type MetricMapperCacheResult struct {
	Mapping *MetricMapping
	Labels  prometheus.Labels
}

type MetricMapperCache struct {
	cache map[string]*MetricMapperCacheResult
}

func NewMetricMapperCache() *MetricMapperCache {
	cacheMutex.Lock()
	defer cacheMutex.Unlock()
	return &MetricMapperCache{cache: make(map[string]*MetricMapperCacheResult)}
}

func (m *MetricMapperCache) Get(metricString string) (*MetricMapperCacheResult, bool) {
	cacheMutex.RLock()
	if result, ok := m.cache[metricString]; ok {
		cacheMutex.RUnlock()
		return result, true
	} else {
		cacheMutex.RUnlock()
		return nil, false
	}
}

func (m *MetricMapperCache) Add(metricString string, mapping *MetricMapping, labels prometheus.Labels) {
	cacheMutex.Lock()
	m.cache[metricString] = &MetricMapperCacheResult{Mapping: mapping, Labels: labels}
	cacheMutex.Unlock()
}
