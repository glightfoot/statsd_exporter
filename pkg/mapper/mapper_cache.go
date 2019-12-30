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
	"bytes"
	"fmt"

	"github.com/VictoriaMetrics/fastcache"
	xdr "github.com/davecgh/go-xdr/xdr2"
	"github.com/prometheus/common/log"

	"github.com/prometheus/client_golang/prometheus"
)

var (
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
}

type MetricMapperCacheResult struct {
	Mapping *MetricMapping
	Matched bool
	Labels  prometheus.Labels
}

type MetricMapperCache struct {
	cache *fastcache.Cache
}

// NewMetricMapperCache returns a new mapping cache
// use named returns to allow returning an error if making a new cache panics (maybe we should just let it panic?)
func NewMetricMapperCache(maxBytes int) (mc *MetricMapperCache, err error) {
	mc = &MetricMapperCache{}
	err = nil
	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("error creating mapping cache: %s", r)
		}
	}()
	mc.cache = fastcache.New(maxBytes)
	return mc, nil
}

func (m *MetricMapperCache) Get(metricString string) (*MetricMapperCacheResult, bool) {
	if encodedData, ok := m.cache.HasGet([]byte{}, []byte(metricString)); ok {
		var result *MetricMapperCacheResult
		_, err := xdr.Unmarshal(bytes.NewReader(encodedData), result)
		if err != nil {
			// TODO: see what might cause an error and handle better
			log.Errorf("Could not unmarshal cached result: %s", err)
			go incrementCachedCounter("miss")
			return nil, false
		}
		go incrementCachedCounter("hit")
		return result, true
	} else {
		go incrementCachedCounter("miss")
		return nil, false
	}
}

func incrementCachedCounter(result string) {
	cachedCounter.WithLabelValues(result).Inc()
}

func (m *MetricMapperCache) AddMatch(metricString string, mapping *MetricMapping, labels prometheus.Labels) {
	var w bytes.Buffer
	v := MetricMapperCacheResult{Mapping: mapping, Matched: true, Labels: labels}
	_, err := xdr.Marshal(&w, &v)
	if err != nil {
		// TODO: handle this error
		log.Errorf("Could not marshal mapping match to add to cache: %s", err)
		return
	}
	encodedData := w.Bytes()
	m.cache.Set([]byte(metricString), encodedData)
}

func (m *MetricMapperCache) AddMiss(metricString string) {
	var w bytes.Buffer
	v := MetricMapperCacheResult{Matched: false}
	_, err := xdr.Marshal(&w, &v)
	if err != nil {
		// TODO: handle this error
		log.Errorf("Could not marshal mapping miss to add to cache: %s", err)
		return
	}
	encodedData := w.Bytes()
	m.cache.Set([]byte(metricString), encodedData)
}
