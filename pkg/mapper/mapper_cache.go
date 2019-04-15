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
	"github.com/hashicorp/golang-lru"

	"github.com/prometheus/client_golang/prometheus"
)

var (
	cacheSize = prometheus.NewGauge(
		prometheus.GaugeOpts{
			Name: "statsd_exporter_cache_size",
			Help: "The count of unique metrics currently cached.",
		},
	)
	cachedCounter = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "statsd_exporter_cache_requests_total",
			Help: "The counter of metric cache hits and misses.",
		},
		[]string{"result"},
	)
)

func init() {
	prometheus.MustRegister(cachedCounter)
	prometheus.MustRegister(cacheSize)
}

type MetricMapperCacheResult struct {
	Mapping *MetricMapping
	Matched bool
	Labels  prometheus.Labels
}

type MetricMapperCache struct {
	cache *lru.Cache
}

func NewMetricMapperCache(size int) (*MetricMapperCache, error) {
	cacheSize.Set(0)
	cache, err := lru.New(size)
	if err != nil {
		return &MetricMapperCache{}, err
	}
	return &MetricMapperCache{cache: cache}, nil
}

func (m *MetricMapperCache) Get(metricString string) (*MetricMapperCacheResult, bool) {
	if result, ok := m.cache.Get(metricString); ok {
		go incrementCachedCounter("hit")
		return result.(*MetricMapperCacheResult), true
	} else {
		go incrementCachedCounter("miss")
		return nil, false
	}
}

func incrementCachedCounter(result string) {
	cachedCounter.WithLabelValues(result).Inc()
}

func (m *MetricMapperCache) AddMatch(metricString string, mapping *MetricMapping, labels prometheus.Labels) {
	m.cache.Add(metricString, &MetricMapperCacheResult{Mapping: mapping, Matched: true, Labels: labels})
	go func() {
		cacheSize.Set(float64(m.cache.Len()))
	}()
}

func (m *MetricMapperCache) AddMiss(metricString string) {
	m.cache.Add(metricString, &MetricMapperCacheResult{Matched: false})
	go func() {
		cacheSize.Set(float64(m.cache.Len()))
	}()
}
