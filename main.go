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
	"bufio"
	"net"
	"net/http"
	_ "net/http/pprof"
	"os"
	"runtime"
	"strconv"
	"strings"
	"time"

	"github.com/howeyc/fsnotify"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/common/log"
	"github.com/prometheus/common/version"
	"gopkg.in/alecthomas/kingpin.v2"

	"github.com/prometheus/statsd_exporter/pkg/mapper"
)

func init() {
	prometheus.MustRegister(version.NewCollector("statsd_exporter"))
}

func serveHTTP(listenAddress, metricsEndpoint string) {
	//lint:ignore SA1019 prometheus.Handler() is deprecated.
	http.Handle(metricsEndpoint, prometheus.Handler())
	http.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`<html>
			<head><title>StatsD Exporter</title></head>
			<body>
			<h1>StatsD Exporter</h1>
			<p><a href="` + metricsEndpoint + `">Metrics</a></p>
			</body>
			</html>`))
	})
	log.Fatal(http.ListenAndServe(listenAddress, nil))
}

func ipPortFromString(addr string) (*net.IPAddr, int) {
	host, portStr, err := net.SplitHostPort(addr)
	if err != nil {
		log.Fatal("Bad StatsD listening address", addr)
	}

	if host == "" {
		host = "0.0.0.0"
	}
	ip, err := net.ResolveIPAddr("ip", host)
	if err != nil {
		log.Fatalf("Unable to resolve %s: %s", host, err)
	}

	port, err := strconv.Atoi(portStr)
	if err != nil || port < 0 || port > 65535 {
		log.Fatalf("Bad port %s: %s", portStr, err)
	}

	return ip, port
}

func udpAddrFromString(addr string) *net.UDPAddr {
	ip, port := ipPortFromString(addr)
	return &net.UDPAddr{
		IP:   ip.IP,
		Port: port,
		Zone: ip.Zone,
	}
}

func tcpAddrFromString(addr string) *net.TCPAddr {
	ip, port := ipPortFromString(addr)
	return &net.TCPAddr{
		IP:   ip.IP,
		Port: port,
		Zone: ip.Zone,
	}
}

func watchConfig(fileName string, mapper *mapper.MetricMapper, cacheSize int64) {
	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		log.Fatal(err)
	}

	err = watcher.WatchFlags(fileName, fsnotify.FSN_MODIFY)
	if err != nil {
		log.Fatal(err)
	}

	for {
		select {
		case ev := <-watcher.Event:
			log.Infof("Config file changed (%s), attempting reload", ev)
			reloaded, err := mapper.InitFromFile(fileName, cacheSize)
			if err != nil {
				log.Errorln("Error reloading config:", err)
				configLoads.WithLabelValues("failure").Inc()
				continue
			}

			if reloaded == true {
				log.Infoln("Config reloaded successfully")
				configLoads.WithLabelValues("success").Inc()
			} else {
				log.Infoln("Config reload skipped")
				configLoads.WithLabelValues("skipped").Inc()
			}
			// Re-add the file watcher since it can get lost on some changes. E.g.
			// saving a file with vim results in a RENAME-MODIFY-DELETE event
			// sequence, after which the newly written file is no longer watched.
			_ = watcher.WatchFlags(fileName, fsnotify.FSN_MODIFY)
		case err := <-watcher.Error:
			log.Errorln("Error watching config:", err)
		}
	}
}

func dumpFSM(mapper *mapper.MetricMapper, dumpFilename string) error {
	f, err := os.Create(dumpFilename)
	if err != nil {
		return err
	}
	log.Infoln("Start dumping FSM to", dumpFilename)
	w := bufio.NewWriter(f)
	mapper.FSM.DumpFSM(w)
	w.Flush()
	f.Close()
	log.Infoln("Finish dumping FSM")
	return nil
}

func watchUDPBuffers(lastDropped int, lastDropped6 int) {
	for {
		myPid := strconv.Itoa(os.Getpid())

		queuedUDP, droppedUDP := parseProcfsNetFile("/proc/" + myPid + "/net/udp")
		label := "udp"

		udpBufferQueued.WithLabelValues(label).Set(float64(queuedUDP))

		diff := droppedUDP - lastDropped
		if diff < 0 {
			log.Info("Dropped count went negative! Abandoning UDP buffer parsing")
			diff = 0
			droppedUDP = lastDropped
		}
		udpBufferDropped.WithLabelValues(label).Add(float64(diff))

		queuedUDP6, droppedUDP6 := parseProcfsNetFile("/proc/" + myPid + "/net/udp6")
		label = "udp6"

		udpBufferQueued.WithLabelValues(label).Set(float64(queuedUDP6))

		diff = droppedUDP6 - lastDropped6
		if diff < 0 {
			log.Info("Dropped count went negative! Abandoning UDP buffer parsing")
			diff = 0
			droppedUDP6 = lastDropped6
		}
		udpBufferDropped.WithLabelValues(label).Add(float64(diff))

		time.Sleep(10 * time.Second)
		lastDropped = droppedUDP
		lastDropped6 = droppedUDP6
	}
}

func parseProcfsNetFile(filename string) (int, int) {
	f, err := os.Open(filename)
	if err != nil {
		return 0, 0
	}
	defer f.Close()

	queued := 0
	dropped := 0
	s := bufio.NewScanner(f)
	for n := 0; s.Scan(); n++ {
		// Skip the header lines.
		if n < 1 {
			continue
		}

		fields := strings.Fields(s.Text())

		queuedLine, err := strconv.ParseInt(strings.Split(fields[4], ":")[1], 16, 32)
		queued = queued + int(queuedLine)
		if err != nil {
			log.Info("Unable to parse queued UDP buffers:", err)
			return 0, 0
		}

		droppedLine, err := strconv.Atoi(fields[12])
		dropped = dropped + droppedLine
		if err != nil {
			log.Info("Unable to parse dropped UDP buffers:", err)
			return 0, 0
		}
	}

	return queued, dropped
}

func main() {
	var (
		listenAddress   = kingpin.Flag("web.listen-address", "The address on which to expose the web interface and generated Prometheus metrics.").Default(":9102").String()
		metricsEndpoint = kingpin.Flag("web.telemetry-path", "Path under which to expose metrics.").Default("/metrics").String()
		statsdListenUDP = kingpin.Flag("statsd.listen-udp", "The UDP address on which to receive statsd metric lines. \"\" disables it.").Default(":9125").String()
		statsdListenTCP = kingpin.Flag("statsd.listen-tcp", "The TCP address on which to receive statsd metric lines. \"\" disables it.").Default(":9125").String()
		mappingConfig   = kingpin.Flag("statsd.mapping-config", "Metric mapping configuration file name.").String()
		readBuffer      = kingpin.Flag("statsd.read-buffer", "Size (in bytes) of the operating system's transmit read buffer associated with the UDP connection. Please make sure the kernel parameters net.core.rmem_max is set to a value greater than the value specified.").Int()
		dumpFSMPath     = kingpin.Flag("debug.dump-fsm", "The path to dump internal FSM generated for glob matching as Dot file.").Default("").String()

		//Concurrency performance tuning
		udpListenerThreads    = kingpin.Flag("udp-listener.threads", "The number of listener threads to receive UDP traffic.").Default("4").Int()
		udpPacketHandlers     = kingpin.Flag("udp-listener.handlers", "The number of concurrent packet handlers").Default("10000").Int()
		eventListenerThreads  = kingpin.Flag("event-listener.threads", "Number of listener threads to handle metric events").Default("1").Int()
		eventListenerHandlers = kingpin.Flag("event-listener.handlers", "Number of listener handlers to handle metric events").Default("1000").Int()

		cacheSize = kingpin.Flag("statsd.cache-size", "Maximum size of your metric cache in human readable bytes (e.g. 1MB, 256MB, 2GB, etc). Mappings are removed from the cache in FIFO order once max size is reached.").Default("256MB").Bytes()
	)

	log.AddFlags(kingpin.CommandLine)
	kingpin.Version(version.Print("statsd_exporter"))
	kingpin.HelpFlag.Short('h')
	kingpin.Parse()

	if *statsdListenUDP == "" && *statsdListenTCP == "" {
		log.Fatalln("At least one of UDP/TCP listeners must be specified.")
	}

	log.Infoln("Starting StatsD -> Prometheus Exporter", version.Info())
	log.Infoln("Build context", version.BuildContext())
	log.Infof("Accepting StatsD Traffic: UDP %v, TCP %v", *statsdListenUDP, *statsdListenTCP)
	log.Infoln("Accepting Prometheus Requests on", *listenAddress)

	go serveHTTP(*listenAddress, *metricsEndpoint)

	var events chan Events
	if *readBuffer != 0 {
		events = make(chan Events, *readBuffer)
	} else {
		events = make(chan Events, 10240)
	}
	defer close(events)

	if *statsdListenUDP != "" {
		udpListenAddr := udpAddrFromString(*statsdListenUDP)
		uconn, err := net.ListenUDP("udp", udpListenAddr)
		if err != nil {
			log.Fatal(err)
		}

		if *readBuffer != 0 {
			err = uconn.SetReadBuffer(*readBuffer)
			if err != nil {
				log.Fatal("Error setting UDP read buffer:", err)
			}
		}

		ul := &StatsDUDPListener{conn: uconn}
		go ul.Listen(*udpListenerThreads, *udpPacketHandlers, events)
	}

	if *statsdListenTCP != "" {
		tcpListenAddr := tcpAddrFromString(*statsdListenTCP)
		tconn, err := net.ListenTCP("tcp", tcpListenAddr)
		if err != nil {
			log.Fatal(err)
		}
		defer tconn.Close()

		tl := &StatsDTCPListener{conn: tconn}
		go tl.Listen(events)
	}

	if runtime.GOOS == "linux" {
		go watchUDPBuffers(0, 0)
	}

	mapper := &mapper.MetricMapper{MappingsCount: mappingsCount}
	if *mappingConfig != "" {
		_, err := mapper.InitFromFile(*mappingConfig, int64(*cacheSize))
		if err != nil {
			log.Fatal("Error loading config:", err)
		}
		if *dumpFSMPath != "" {
			err := dumpFSM(mapper, *dumpFSMPath)
			if err != nil {
				log.Fatal("Error dumping FSM:", err)
			}
		}
		go watchConfig(*mappingConfig, mapper, int64(*cacheSize))
	} else {
		mapper.InitCache(int64(*cacheSize))
	}
	exporter := NewExporter(mapper)
	exporter.Listen(*eventListenerThreads, *eventListenerHandlers, events)
}
