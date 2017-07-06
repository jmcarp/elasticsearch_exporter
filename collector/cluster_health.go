package collector

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"time"

	"github.com/go-kit/kit/log"
	"github.com/go-kit/kit/log/level"
	"github.com/prometheus/client_golang/prometheus"
)

const (
	namespace = "elasticsearch"
)

var (
	colors                     = []string{"green", "yellow", "red"}
	defaultClusterHealthLabels = []string{"cluster"}
)

type clusterHealthMetric struct {
	Type  prometheus.ValueType
	Desc  *prometheus.Desc
	Value func(clusterHealth clusterHealthResponse) float64
}

type clusterHealthStatusMetric struct {
	Type   prometheus.ValueType
	Desc   *prometheus.Desc
	Value  func(clusterHealth clusterHealthResponse, color string) float64
	Labels func(clusterName, color string) []string
}

type ClusterHealth struct {
	logger log.Logger
	client *http.Client
	url    *url.URL

	metrics      []*clusterHealthMetric
	statusMetric *clusterHealthStatusMetric

	totalScrapesMetric              prometheus.Counter
	totalScrapeErrorsMetric         prometheus.Counter
	lastScrapeErrorMetric           prometheus.Gauge
	lastScrapeTimestampMetric       prometheus.Gauge
	lastScrapeDurationSecondsMetric prometheus.Gauge
}

func NewClusterHealth(logger log.Logger, client *http.Client, url *url.URL) *ClusterHealth {
	subsystem := "cluster_health"

	return &ClusterHealth{
		logger: logger,
		client: client,
		url:    url,

		metrics: []*clusterHealthMetric{
			{
				Type: prometheus.GaugeValue,
				Desc: prometheus.NewDesc(
					prometheus.BuildFQName(namespace, subsystem, "active_primary_shards"),
					"Tthe number of primary shards in your cluster. This is an aggregate total across all indices.",
					defaultClusterHealthLabels, nil,
				),
				Value: func(clusterHealth clusterHealthResponse) float64 {
					return float64(clusterHealth.ActivePrimaryShards)
				},
			},
			{
				Type: prometheus.GaugeValue,
				Desc: prometheus.NewDesc(
					prometheus.BuildFQName(namespace, subsystem, "active_shards"),
					"Aggregate total of all shards across all indices, which includes replica shards.",
					defaultClusterHealthLabels, nil,
				),
				Value: func(clusterHealth clusterHealthResponse) float64 {
					return float64(clusterHealth.ActiveShards)
				},
			},
			{
				Type: prometheus.GaugeValue,
				Desc: prometheus.NewDesc(
					prometheus.BuildFQName(namespace, subsystem, "delayed_unassigned_shards"),
					"Shards delayed to reduce reallocation overhead",
					defaultClusterHealthLabels, nil,
				),
				Value: func(clusterHealth clusterHealthResponse) float64 {
					return float64(clusterHealth.DelayedUnassignedShards)
				},
			},
			{
				Type: prometheus.GaugeValue,
				Desc: prometheus.NewDesc(
					prometheus.BuildFQName(namespace, subsystem, "initializing_shards"),
					"Count of shards that are being freshly created.",
					defaultClusterHealthLabels, nil,
				),
				Value: func(clusterHealth clusterHealthResponse) float64 {
					return float64(clusterHealth.InitializingShards)
				},
			},
			{
				Type: prometheus.GaugeValue,
				Desc: prometheus.NewDesc(
					prometheus.BuildFQName(namespace, subsystem, "number_of_data_nodes"),
					"Number of data nodes in the cluster.",
					defaultClusterHealthLabels, nil,
				),
				Value: func(clusterHealth clusterHealthResponse) float64 {
					return float64(clusterHealth.NumberOfDataNodes)
				},
			},
			{
				Type: prometheus.GaugeValue,
				Desc: prometheus.NewDesc(
					prometheus.BuildFQName(namespace, subsystem, "number_of_in_flight_fetch"),
					"The number of ongoing shard info requests.",
					defaultClusterHealthLabels, nil,
				),
				Value: func(clusterHealth clusterHealthResponse) float64 {
					return float64(clusterHealth.NumberOfInFlightFetch)
				},
			},
			{
				Type: prometheus.GaugeValue,
				Desc: prometheus.NewDesc(
					prometheus.BuildFQName(namespace, subsystem, "number_of_nodes"),
					"Number of nodes in the cluster.",
					defaultClusterHealthLabels, nil,
				),
				Value: func(clusterHealth clusterHealthResponse) float64 {
					return float64(clusterHealth.NumberOfNodes)
				},
			},
			{
				Type: prometheus.GaugeValue,
				Desc: prometheus.NewDesc(
					prometheus.BuildFQName(namespace, subsystem, "number_of_pending_tasks"),
					"Cluster level changes which have not yet been executed",
					defaultClusterHealthLabels, nil,
				),
				Value: func(clusterHealth clusterHealthResponse) float64 {
					return float64(clusterHealth.NumberOfPendingTasks)
				},
			},
			{
				Type: prometheus.GaugeValue,
				Desc: prometheus.NewDesc(
					prometheus.BuildFQName(namespace, subsystem, "relocating_shards"),
					"The number of shards that are currently moving from one node to another node.",
					defaultClusterHealthLabels, nil,
				),
				Value: func(clusterHealth clusterHealthResponse) float64 {
					return float64(clusterHealth.RelocatingShards)
				},
			},
			{
				Type: prometheus.GaugeValue,
				Desc: prometheus.NewDesc(
					prometheus.BuildFQName(namespace, subsystem, "timed_out"),
					"Number of cluster health checks timed out",
					defaultClusterHealthLabels, nil,
				),
				Value: func(clusterHealth clusterHealthResponse) float64 {
					if clusterHealth.TimedOut {
						return 1
					}
					return 0
				},
			},
			{
				Type: prometheus.GaugeValue,
				Desc: prometheus.NewDesc(
					prometheus.BuildFQName(namespace, subsystem, "unassigned_shards"),
					"The number of shards that exist in the cluster state, but cannot be found in the cluster itself.",
					defaultClusterHealthLabels, nil,
				),
				Value: func(clusterHealth clusterHealthResponse) float64 {
					return float64(clusterHealth.UnassignedShards)
				},
			},
		},
		statusMetric: &clusterHealthStatusMetric{
			Type: prometheus.GaugeValue,
			Desc: prometheus.NewDesc(
				prometheus.BuildFQName(namespace, subsystem, "status"),
				"Whether all primary and replica shards are allocated.",
				[]string{"cluster", "color"}, nil,
			),
			Value: func(clusterHealth clusterHealthResponse, color string) float64 {
				if clusterHealth.Status == color {
					return 1
				}
				return 0
			},
		},
		totalScrapesMetric: prometheus.NewCounter(
			prometheus.CounterOpts{
				Namespace: namespace,
				Subsystem: "cluster_health",
				Name:      "scrapes_total",
				Help:      "Total number of times ElasticSearch cluster health was scraped for metrics.",
				ConstLabels: prometheus.Labels{
					"url": url.String(),
				},
			},
		),
		totalScrapeErrorsMetric: prometheus.NewCounter(
			prometheus.CounterOpts{
				Namespace: namespace,
				Subsystem: "cluster_health",
				Name:      "scrape_errors_total",
				Help:      "Total number of times an error occured scraping ElasticSearch cluster health.",
				ConstLabels: prometheus.Labels{
					"url": url.String(),
				},
			},
		),
		lastScrapeErrorMetric: prometheus.NewGauge(
			prometheus.GaugeOpts{
				Namespace: namespace,
				Subsystem: "cluster_health",
				Name:      "last_scrape_error",
				Help:      "Whether the last scrape of metrics from ElasticSearch cluster health resulted in an error (1 for error, 0 for success).",
				ConstLabels: prometheus.Labels{
					"url": url.String(),
				},
			},
		),
		lastScrapeTimestampMetric: prometheus.NewGauge(
			prometheus.GaugeOpts{
				Namespace: namespace,
				Subsystem: "cluster_health",
				Name:      "last_scrape_timestamp",
				Help:      "Number of seconds since 1970 since last scrape from ElasticSearch cluster health.",
				ConstLabels: prometheus.Labels{
					"url": url.String(),
				},
			},
		),
		lastScrapeDurationSecondsMetric: prometheus.NewGauge(
			prometheus.GaugeOpts{
				Namespace: namespace,
				Subsystem: "cluster_health",
				Name:      "last_scrape_duration_seconds",
				Help:      "Duration of the last scrape from ElasticSearch cluster health.",
				ConstLabels: prometheus.Labels{
					"url": url.String(),
				},
			},
		),
	}
}

func (c *ClusterHealth) Describe(ch chan<- *prometheus.Desc) {
	for _, metric := range c.metrics {
		ch <- metric.Desc
	}
	ch <- c.statusMetric.Desc
}

func (c *ClusterHealth) fetchAndDecodeClusterHealth() (clusterHealthResponse, error) {
	var chr clusterHealthResponse

	u := *c.url
	u.Path = "/_cluster/health"
	res, err := c.client.Get(u.String())
	if err != nil {
		return chr, fmt.Errorf("failed to get cluster health from %s: %s", u.String(), err)
	}
	defer res.Body.Close()

	if res.StatusCode != http.StatusOK {
		return chr, fmt.Errorf("HTTP Request failed with code %d", res.StatusCode)
	}

	if err := json.NewDecoder(res.Body).Decode(&chr); err != nil {
		return chr, err
	}

	return chr, nil
}

func (c *ClusterHealth) Collect(ch chan<- prometheus.Metric) {
	begun := time.Now()
	scrapeError := 0
	c.totalScrapesMetric.Inc()

	clusterHealthResponse, err := c.fetchAndDecodeClusterHealth()
	if err != nil {
		level.Warn(c.logger).Log(
			"msg", "failed to fetch and decode cluster health",
			"err", err,
		)
		scrapeError = 1
		c.totalScrapeErrorsMetric.Inc()
	}

	for _, metric := range c.metrics {
		ch <- prometheus.MustNewConstMetric(
			metric.Desc,
			metric.Type,
			metric.Value(clusterHealthResponse),
			clusterHealthResponse.ClusterName,
		)
	}

	for _, color := range colors {
		ch <- prometheus.MustNewConstMetric(
			c.statusMetric.Desc,
			c.statusMetric.Type,
			c.statusMetric.Value(clusterHealthResponse, color),
			clusterHealthResponse.ClusterName, color,
		)
	}

	c.totalScrapesMetric.Collect(ch)
	c.totalScrapeErrorsMetric.Collect(ch)

	c.lastScrapeErrorMetric.Set(float64(scrapeError))
	c.lastScrapeErrorMetric.Collect(ch)

	c.lastScrapeTimestampMetric.Set(float64(time.Now().Unix()))
	c.lastScrapeTimestampMetric.Collect(ch)

	c.lastScrapeDurationSecondsMetric.Set(time.Since(begun).Seconds())
	c.lastScrapeDurationSecondsMetric.Collect(ch)
}
