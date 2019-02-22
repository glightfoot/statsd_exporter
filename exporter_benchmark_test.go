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

package main

import (
	"fmt"
	"sync"
	"testing"

	"github.com/prometheus/statsd_exporter/pkg/mapper"
)

func benchmarkExporter(times int, b *testing.B) {
	input := []string{
		"foo1:2|c",
		"foo2:3|g",
		"foo3:200|ms",
		"foo4:100|c|#tag1:bar,tag2:baz",
		"foo5:100|c|#tag1:bar,#tag2:baz",
		"foo6:100|c|#09digits:0,tag.with.dots:1",
		"foo10:100|c|@0.1|#tag1:bar,#tag2:baz",
		"foo11:100|c|@0.1|#tag1:foo:bar",
		"foo15:200|ms:300|ms:5|c|@0.1:6|g\nfoo15a:1|c:5|ms",
		"some_very_useful_metrics_with_quite_a_log_name:13|c",
	}
	bytesInput := make([]string, len(input)*times)
	for run := 0; run < times; run++ {
		for i := 0; i < len(input); i++ {
			bytesInput[run*len(input)+i] = fmt.Sprintf("run%d%s", run, input[i])
		}
	}
	for n := 0; n < b.N; n++ {
		l := StatsDUDPListener{}
		// there are more events than input lines, need bigger buffer
		events := make(chan Events, len(bytesInput)*times*2)

		for i := 0; i < times; i++ {
			for _, line := range bytesInput {
				l.handlePacket([]byte(line), events)
			}
		}
	}
}

// func BenchmarkExporter1(b *testing.B) {
// 	benchmarkExporter(1, b)
// }
// func BenchmarkExporter5(b *testing.B) {
// 	benchmarkExporter(5, b)
// }
// func BenchmarkExporter50(b *testing.B) {
// 	benchmarkExporter(50, b)
// }

func benchmarkExporterListen(times int, b *testing.B) {
	input := []string{
		"foo1:2|c",
		"foo2:3|g",
		"foo3:200|ms",
		"foo4:100|c|#tag1:bar,tag2:baz",
		"foo5:100|c|#tag1:bar,#tag2:baz",
		"foo6:100|c|#09digits:0,tag.with.dots:1",
		"foo10:100|c|@0.1|#tag1:bar,#tag2:baz",
		"foo11:100|c|@0.1|#tag1:foo:bar",
		"foo15:200|ms:300|ms:5|c|@0.1:6|g\nfoo15a:1|c:5|ms",
		"some_very_useful_metrics_with_quite_a_log_name:13|c",
	}
	bytesInput := make([]string, len(input)*times)
	for run := 0; run < times; run++ {
		for i := 0; i < len(input); i++ {
			bytesInput[run*len(input)+i] = fmt.Sprintf("run%d%s", run, input[i])
		}
	}
	mapper := &mapper.MetricMapper{MappingsCount: mappingsCount}
	exporter := NewExporter(mapper)

	events := make(chan Events, 12)

	b.ResetTimer()

	for n := 0; n < b.N; n++ {
		// there are more events than input lines, need bigger buffer
		wg := sync.WaitGroup{}

		for i := 0; i < times; i++ {
			for _, line := range bytesInput {
				wg.Add(2)
				go func() {
					defer wg.Done()
					for _, event := range lineToEvents(line) {
						exporter.handleEvent(event)
					}
				}()
				go func() {
					defer wg.Done()
					for _, event := range lineToEvents(line) {
						exporter.handleEvent(event)
					}
				}()
				// go func() { events <- lineToEvents("foo16:2|c") }()
				// go func() { events <- lineToEvents("foo17:2|c") }()

				for _, event := range lineToEvents(line) {
					exporter.handleEvent(event)
				}
				// for _, event := range lineToEvents(line) {
				// 	exporter.handleEvent(event)
				// }
				// for _, event := range lineToEvents(line) {
				// 	exporter.handleEvent(event)
				// }
				wg.Wait()
			}
		}

	}
	close(events)
}

func BenchmarkExporterListen1(b *testing.B) {
	benchmarkExporterListen(1, b)
}
func BenchmarkExporterListen5(b *testing.B) {
	benchmarkExporterListen(5, b)
}
func BenchmarkExporterListen50(b *testing.B) {
	benchmarkExporterListen(50, b)
}
