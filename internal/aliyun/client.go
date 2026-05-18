package aliyun

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha1"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"
)

const (
	SiteChina               = "china"
	SiteInternational       = "international"
	CDTScopeMainland        = "mainland"
	CDTScopeOverseas        = "overseas"
	StoppedModeStopCharging = "StopCharging"
	StoppedModeKeepCharging = "KeepCharging"
)

type Client struct {
	accessKeyID     string
	accessKeySecret string
	site            string
	httpClient      *http.Client
}

type RPCRequest struct {
	Host    string
	Version string
	Action  string
	Query   map[string]string
}

type Region struct {
	RegionID  string
	LocalName string
}

type Instance struct {
	InstanceID              string
	InstanceName            string
	Status                  string
	RegionID                string
	RegionName              string
	InstanceType            string
	InstanceChargeType      string
	CPU                     int
	Memory                  int
	SpotStrategy            string
	PublicIP                string
	IPv6Addresses           []string
	PrivateIP               string
	InternetMaxBandwidthIn  int
	InternetMaxBandwidthOut int
}

func (i Instance) IsSpot() bool {
	strategy := strings.TrimSpace(i.SpotStrategy)
	return strategy != "" && strategy != "NoSpot"
}

type TrafficResult struct {
	GB           float64
	Source       string
	Points       int
	Metric       string
	MainlandGB   float64
	OverseasGB   float64
	RegionUsages []CDTRegionUsage
}

type CDTRegionUsage struct {
	RegionID string
	Scope    string
	GB       float64
}

type MetricPoint struct {
	Timestamp int64
	Average   float64
	Maximum   float64
	Minimum   float64
}

func NewClient(accessKeyID, accessKeySecret, site string, timeout time.Duration) *Client {
	if timeout <= 0 {
		timeout = 20 * time.Second
	}
	return &Client{
		accessKeyID:     accessKeyID,
		accessKeySecret: accessKeySecret,
		site:            site,
		httpClient:      &http.Client{Timeout: timeout},
	}
}

func (c *Client) BuildRPCRequest(ctx context.Context, rpc RPCRequest) (*http.Request, error) {
	if rpc.Host == "" || rpc.Version == "" || rpc.Action == "" {
		return nil, fmt.Errorf("RPC request 缺少 host/version/action")
	}
	params := map[string]string{
		"Format":           "JSON",
		"Version":          rpc.Version,
		"AccessKeyId":      c.accessKeyID,
		"SignatureMethod":  "HMAC-SHA1",
		"Timestamp":        time.Now().UTC().Format("2006-01-02T15:04:05Z"),
		"SignatureVersion": "1.0",
		"SignatureNonce":   nonce(),
		"Action":           rpc.Action,
	}
	for key, value := range rpc.Query {
		params[key] = value
	}
	params["Signature"] = c.sign(params)

	values := url.Values{}
	for key, value := range params {
		values.Set(key, value)
	}
	endpoint := url.URL{Scheme: "https", Host: rpc.Host, Path: "/", RawQuery: values.Encode()}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint.String(), nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/json")
	return req, nil
}

func (c *Client) DescribeRegions(ctx context.Context) ([]Region, error) {
	var response struct {
		Regions struct {
			Region []struct {
				RegionID  string `json:"RegionId"`
				LocalName string `json:"LocalName"`
			} `json:"Region"`
		} `json:"Regions"`
	}
	err := c.doRPC(ctx, RPCRequest{
		Host:    ecsHost(c.defaultECSRegion()),
		Version: "2014-05-26",
		Action:  "DescribeRegions",
	}, &response)
	if err != nil {
		return nil, err
	}
	regions := make([]Region, 0, len(response.Regions.Region))
	for _, item := range response.Regions.Region {
		if item.RegionID == "" {
			continue
		}
		name := item.LocalName
		if name == "" {
			name = item.RegionID
		}
		regions = append(regions, Region{RegionID: item.RegionID, LocalName: name})
	}
	return regions, nil
}

func (c *Client) DescribeInstances(ctx context.Context, region Region) ([]Instance, error) {
	var instances []Instance
	pageNumber := 1
	for {
		var response struct {
			TotalCount int `json:"TotalCount"`
			PageSize   int `json:"PageSize"`
			Instances  struct {
				Instance []rawInstance `json:"Instance"`
			} `json:"Instances"`
		}
		err := c.doRPC(ctx, RPCRequest{
			Host:    ecsHost(region.RegionID),
			Version: "2014-05-26",
			Action:  "DescribeInstances",
			Query:   describeInstancesQuery(region.RegionID, pageNumber),
		}, &response)
		if err != nil {
			return nil, err
		}
		for _, item := range response.Instances.Instance {
			instances = append(instances, item.toInstance(region))
		}
		if response.PageSize <= 0 || pageNumber*response.PageSize >= response.TotalCount {
			break
		}
		pageNumber++
	}
	return instances, nil
}

func (c *Client) DescribeNetworkInterfaceIPv6(ctx context.Context, regionID string) (map[string][]string, error) {
	result := map[string][]string{}
	pageNumber := 1
	for {
		var response struct {
			TotalCount           int `json:"TotalCount"`
			PageSize             int `json:"PageSize"`
			NetworkInterfaceSets struct {
				NetworkInterfaceSet []struct {
					InstanceID string      `json:"InstanceId"`
					IPv6Sets   rawIPv6Sets `json:"Ipv6Sets"`
				} `json:"NetworkInterfaceSet"`
			} `json:"NetworkInterfaceSets"`
		}
		err := c.doRPC(ctx, RPCRequest{
			Host:    ecsHost(regionID),
			Version: "2014-05-26",
			Action:  "DescribeNetworkInterfaces",
			Query: map[string]string{
				"RegionId":   regionID,
				"PageSize":   "100",
				"PageNumber": strconv.Itoa(pageNumber),
			},
		}, &response)
		if err != nil {
			return nil, err
		}
		for _, item := range response.NetworkInterfaceSets.NetworkInterfaceSet {
			if item.InstanceID == "" {
				continue
			}
			result[item.InstanceID] = append(result[item.InstanceID], item.IPv6Sets.addresses()...)
			result[item.InstanceID] = uniqueStrings(result[item.InstanceID])
		}
		if response.PageSize <= 0 || pageNumber*response.PageSize >= response.TotalCount {
			break
		}
		pageNumber++
	}
	return result, nil
}

func (c *Client) StartInstance(ctx context.Context, regionID, instanceID string) error {
	return c.doRPC(ctx, RPCRequest{
		Host:    ecsHost(regionID),
		Version: "2014-05-26",
		Action:  "StartInstance",
		Query: map[string]string{
			"RegionId":   regionID,
			"InstanceId": instanceID,
		},
	}, nil)
}

func (c *Client) StopInstance(ctx context.Context, regionID, instanceID, stoppedMode string) error {
	query := map[string]string{
		"RegionId":   regionID,
		"InstanceId": instanceID,
	}
	if stoppedMode != "" {
		query["StoppedMode"] = stoppedMode
	}
	return c.doRPC(ctx, RPCRequest{
		Host:    ecsHost(regionID),
		Version: "2014-05-26",
		Action:  "StopInstance",
		Query:   query,
	}, nil)
}

func (c *Client) CdtTrafficGBForRegion(ctx context.Context, targetRegion string) (TrafficResult, error) {
	var response struct {
		TrafficDetails []rawCDTTrafficDetail `json:"TrafficDetails"`
	}
	err := c.doRPC(ctx, RPCRequest{
		Host:    "cdt.aliyuncs.com",
		Version: "2021-08-13",
		Action:  "ListCdtInternetTraffic",
		Query: map[string]string{
			"RegionId": c.defaultCDTRegion(),
		},
	}, &response)
	if err != nil {
		return TrafficResult{}, err
	}
	_, mainlandGB, overseasGB, regionUsages := splitCDTTrafficGB(response.TrafficDetails)
	scope := CDTScopeForRegion(targetRegion)
	result := TrafficResult{GB: overseasGB, Source: "cdt", MainlandGB: mainlandGB, OverseasGB: overseasGB, RegionUsages: regionUsages}
	if scope == CDTScopeMainland {
		result.GB = mainlandGB
	}
	return result, nil
}

func (c *Client) CdtAccountTrafficGB(ctx context.Context) (TrafficResult, error) {
	var response struct {
		TrafficDetails []rawCDTTrafficDetail `json:"TrafficDetails"`
	}
	err := c.doRPC(ctx, RPCRequest{
		Host:    "cdt.aliyuncs.com",
		Version: "2021-08-13",
		Action:  "ListCdtInternetTraffic",
		Query: map[string]string{
			"RegionId": c.defaultCDTRegion(),
		},
	}, &response)
	if err != nil {
		return TrafficResult{}, err
	}
	totalGB, mainlandGB, overseasGB, regionUsages := splitCDTTrafficGB(response.TrafficDetails)
	return TrafficResult{GB: totalGB, Source: "cdt", MainlandGB: mainlandGB, OverseasGB: overseasGB, RegionUsages: regionUsages}, nil
}

func (c *Client) InstanceOutboundTrafficGB(ctx context.Context, instance Instance, start, end time.Time) (TrafficResult, error) {
	startMs := start.UnixMilli()
	endMs := end.UnixMilli()
	if endMs <= startMs {
		return TrafficResult{Source: "cms"}, nil
	}

	candidates := []metricCandidate{}
	if instance.PublicIP != "" {
		candidates = append(candidates, metricCandidate{
			Name: "VPC_PublicIP_InternetOutRate",
			Dimensions: []map[string]string{{
				"instanceId": instance.InstanceID,
				"ip":         instance.PublicIP,
			}},
		})
	}
	candidates = append(candidates, metricCandidate{
		Name: "VPC_PublicIP_InternetOutRate",
		Dimensions: []map[string]string{{
			"instanceId": instance.InstanceID,
		}},
	})
	candidates = append(candidates, metricCandidate{
		Name: "InternetOutRate",
		Dimensions: []map[string]string{{
			"instanceId": instance.InstanceID,
		}},
	})

	var lastErr error
	for _, candidate := range candidates {
		result, err := c.queryMetricRateAsTraffic(ctx, candidate, startMs, endMs)
		if err != nil {
			lastErr = err
			continue
		}
		if result.Points > 0 || candidate.Name == "InternetOutRate" {
			return result, nil
		}
	}
	if lastErr != nil {
		return TrafficResult{}, lastErr
	}
	return TrafficResult{Source: "cms", Metric: "InternetOutRate"}, nil
}

func (c *Client) queryMetricRateAsTraffic(ctx context.Context, candidate metricCandidate, startMs, endMs int64) (TrafficResult, error) {
	const periodSeconds = 3600
	const chunkMs int64 = 31 * 24 * 3600 * 1000
	cursor := startMs
	totalBytes := 0.0
	points := 0

	dimensions, err := json.Marshal(candidate.Dimensions)
	if err != nil {
		return TrafficResult{}, err
	}
	for cursor < endMs {
		chunkEnd := minInt64(cursor+chunkMs, endMs)
		var response struct {
			Datapoints json.RawMessage `json:"Datapoints"`
			NextToken  string          `json:"NextToken"`
		}
		err := c.doRPC(ctx, RPCRequest{
			Host:    "metrics.aliyuncs.com",
			Version: "2019-01-01",
			Action:  "DescribeMetricList",
			Query: map[string]string{
				"RegionId":   c.defaultECSRegion(),
				"Namespace":  "acs_ecs_dashboard",
				"MetricName": candidate.Name,
				"Period":     strconv.Itoa(periodSeconds),
				"StartTime":  strconv.FormatInt(cursor, 10),
				"EndTime":    strconv.FormatInt(chunkEnd, 10),
				"Dimensions": string(dimensions),
				"Length":     "1000",
			},
		}, &response)
		if err != nil {
			return TrafficResult{}, err
		}
		metricPoints, err := parseMetricPoints(response.Datapoints)
		if err != nil {
			return TrafficResult{}, err
		}
		sort.Slice(metricPoints, func(i, j int) bool {
			return metricPoints[i].Timestamp < metricPoints[j].Timestamp
		})
		for _, point := range metricPoints {
			if point.Timestamp <= startMs || point.Timestamp > endMs {
				continue
			}
			rateBitsPerSecond := firstPositive(point.Average, point.Maximum, point.Minimum)
			totalBytes += (rateBitsPerSecond * periodSeconds) / 8
			points++
		}
		cursor = chunkEnd
	}

	return TrafficResult{
		GB:     totalBytes / 1024 / 1024 / 1024,
		Source: "cms",
		Points: points,
		Metric: candidate.Name,
	}, nil
}

func (c *Client) doRPC(ctx context.Context, rpc RPCRequest, out any) error {
	req, err := c.BuildRPCRequest(ctx, rpc)
	if err != nil {
		return err
	}
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("aliyun %s HTTP %d: %s", rpc.Action, resp.StatusCode, strings.TrimSpace(string(body)))
	}
	if len(body) == 0 || out == nil {
		return nil
	}
	var apiErr struct {
		Code    string `json:"Code"`
		Message string `json:"Message"`
	}
	if err := json.Unmarshal(body, &apiErr); err == nil && apiErr.Code != "" && apiErr.Code != "200" && apiErr.Code != "OK" {
		return fmt.Errorf("aliyun %s %s: %s", rpc.Action, apiErr.Code, apiErr.Message)
	}
	if err := json.Unmarshal(body, out); err != nil {
		return fmt.Errorf("解析 %s 响应失败: %w", rpc.Action, err)
	}
	return nil
}

func (c *Client) sign(params map[string]string) string {
	keys := make([]string, 0, len(params))
	for key := range params {
		keys = append(keys, key)
	}
	sort.Strings(keys)

	pairs := make([]string, 0, len(keys))
	for _, key := range keys {
		pairs = append(pairs, percentEncode(key)+"="+percentEncode(params[key]))
	}
	canonicalizedQueryString := strings.Join(pairs, "&")
	stringToSign := "POST&%2F&" + percentEncode(canonicalizedQueryString)
	mac := hmac.New(sha1.New, []byte(c.accessKeySecret+"&"))
	_, _ = mac.Write([]byte(stringToSign))
	return base64.StdEncoding.EncodeToString(mac.Sum(nil))
}

type rawInstance struct {
	InstanceID              string `json:"InstanceId"`
	InstanceName            string `json:"InstanceName"`
	Status                  string `json:"Status"`
	InstanceType            string `json:"InstanceType"`
	InstanceChargeType      string `json:"InstanceChargeType"`
	CPU                     int    `json:"Cpu"`
	Memory                  int    `json:"Memory"`
	SpotStrategy            string `json:"SpotStrategy"`
	InternetMaxBandwidthIn  int    `json:"InternetMaxBandwidthIn"`
	InternetMaxBandwidthOut int    `json:"InternetMaxBandwidthOut"`
	PublicIPAddress         struct {
		IPAddress []string `json:"IpAddress"`
	} `json:"PublicIpAddress"`
	EIPAddress struct {
		IPAddress    string `json:"IpAddress"`
		AllocationID string `json:"AllocationId"`
		Bandwidth    int    `json:"Bandwidth"`
	} `json:"EipAddress"`
	VPCAttributes struct {
		PrivateIPAddress struct {
			IPAddress []string `json:"IpAddress"`
		} `json:"PrivateIpAddress"`
		IPv6Address struct {
			IPv6Address []string `json:"Ipv6Address"`
		} `json:"Ipv6Address"`
	} `json:"VpcAttributes"`
	IPv6Address struct {
		IPv6Address []string `json:"Ipv6Address"`
	} `json:"Ipv6Address"`
	NetworkInterfaces struct {
		NetworkInterface []rawNetworkInterface `json:"NetworkInterface"`
	} `json:"NetworkInterfaces"`
}

func (r rawInstance) toInstance(region Region) Instance {
	publicIP := firstString(r.PublicIPAddress.IPAddress)
	if r.EIPAddress.IPAddress != "" {
		publicIP = r.EIPAddress.IPAddress
	}
	privateIP := firstString(r.VPCAttributes.PrivateIPAddress.IPAddress)
	bandwidth := r.InternetMaxBandwidthOut
	if r.EIPAddress.Bandwidth > 0 {
		bandwidth = r.EIPAddress.Bandwidth
	}
	ipv6Addresses := uniqueStrings(r.IPv6Address.IPv6Address)
	ipv6Addresses = append(ipv6Addresses, r.VPCAttributes.IPv6Address.IPv6Address...)
	for _, networkInterface := range r.NetworkInterfaces.NetworkInterface {
		ipv6Addresses = append(ipv6Addresses, networkInterface.IPv6Sets.addresses()...)
	}
	ipv6Addresses = uniqueStrings(ipv6Addresses)
	return Instance{
		InstanceID:              r.InstanceID,
		InstanceName:            r.InstanceName,
		Status:                  r.Status,
		RegionID:                region.RegionID,
		RegionName:              region.LocalName,
		InstanceType:            r.InstanceType,
		InstanceChargeType:      r.InstanceChargeType,
		CPU:                     r.CPU,
		Memory:                  r.Memory,
		SpotStrategy:            r.SpotStrategy,
		PublicIP:                publicIP,
		IPv6Addresses:           ipv6Addresses,
		PrivateIP:               privateIP,
		InternetMaxBandwidthIn:  r.InternetMaxBandwidthIn,
		InternetMaxBandwidthOut: bandwidth,
	}
}

type rawNetworkInterface struct {
	IPv6Sets rawIPv6Sets `json:"Ipv6Sets"`
}

type rawCDTTrafficDetail struct {
	BusinessRegionID string  `json:"BusinessRegionId"`
	Traffic          float64 `json:"Traffic"`
}

type rawIPv6Sets struct {
	IPv6Set     []rawIPv6Set `json:"Ipv6Set"`
	IPv6Address []string     `json:"Ipv6Address"`
}

type rawIPv6Set struct {
	IPv6Address string `json:"Ipv6Address"`
}

func (s rawIPv6Sets) addresses() []string {
	values := append([]string(nil), s.IPv6Address...)
	for _, item := range s.IPv6Set {
		values = append(values, item.IPv6Address)
	}
	return uniqueStrings(values)
}

type metricCandidate struct {
	Name       string
	Dimensions []map[string]string
}

func describeInstancesQuery(regionID string, pageNumber int) map[string]string {
	return map[string]string{
		"RegionId":               regionID,
		"PageSize":               "100",
		"PageNumber":             strconv.Itoa(pageNumber),
		"AdditionalAttributes.1": "NETWORK_PRIMARY_ENI_IP",
	}
}

func parseMetricPoints(raw json.RawMessage) ([]MetricPoint, error) {
	if len(raw) == 0 || string(raw) == "null" {
		return nil, nil
	}
	var text string
	if err := json.Unmarshal(raw, &text); err == nil {
		raw = []byte(text)
	}
	if len(raw) == 0 {
		return nil, nil
	}
	var points []MetricPoint
	if err := json.Unmarshal(raw, &points); err != nil {
		return nil, err
	}
	return points, nil
}

func percentEncode(value string) string {
	escaped := url.QueryEscape(value)
	escaped = strings.ReplaceAll(escaped, "+", "%20")
	escaped = strings.ReplaceAll(escaped, "*", "%2A")
	escaped = strings.ReplaceAll(escaped, "%7E", "~")
	return escaped
}

func nonce() string {
	var bytes [16]byte
	if _, err := rand.Read(bytes[:]); err == nil {
		return hex.EncodeToString(bytes[:])
	}
	return strconv.FormatInt(time.Now().UnixNano(), 10)
}

func ecsHost(regionID string) string {
	return "ecs." + regionID + ".aliyuncs.com"
}

func (c *Client) defaultECSRegion() string {
	if c.site == SiteInternational {
		return "ap-southeast-1"
	}
	return "cn-hangzhou"
}

func (c *Client) defaultCDTRegion() string {
	if c.site == SiteInternational {
		return "ap-southeast-1"
	}
	return "cn-hongkong"
}

func CDTScopeForRegion(regionID string) string {
	if isMainlandRegion(regionID) {
		return CDTScopeMainland
	}
	return CDTScopeOverseas
}

func isMainlandRegion(regionID string) bool {
	return strings.HasPrefix(regionID, "cn-") && regionID != "cn-hongkong"
}

func isOverseas(regionID string) bool {
	return !isMainlandRegion(regionID)
}

func splitCDTTrafficGB(details []rawCDTTrafficDetail) (float64, float64, float64, []CDTRegionUsage) {
	var totalBytes float64
	var mainlandBytes float64
	var overseasBytes float64
	regionBytes := map[string]float64{}
	for _, item := range details {
		if item.BusinessRegionID == "" {
			continue
		}
		totalBytes += item.Traffic
		regionBytes[item.BusinessRegionID] += item.Traffic
		if isMainlandRegion(item.BusinessRegionID) {
			mainlandBytes += item.Traffic
			continue
		}
		overseasBytes += item.Traffic
	}
	regions := make([]CDTRegionUsage, 0, len(regionBytes))
	for regionID, trafficBytes := range regionBytes {
		regions = append(regions, CDTRegionUsage{
			RegionID: regionID,
			Scope:    CDTScopeForRegion(regionID),
			GB:       bytesToGB(trafficBytes),
		})
	}
	sort.Slice(regions, func(i, j int) bool {
		if regions[i].GB == regions[j].GB {
			return regions[i].RegionID < regions[j].RegionID
		}
		return regions[i].GB > regions[j].GB
	})
	return bytesToGB(totalBytes), bytesToGB(mainlandBytes), bytesToGB(overseasBytes), regions
}

func bytesToGB(value float64) float64 {
	return value / 1024 / 1024 / 1024
}

func firstString(values []string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
}

func uniqueStrings(values []string) []string {
	seen := map[string]bool{}
	result := make([]string, 0, len(values))
	for _, value := range values {
		if value == "" || seen[value] {
			continue
		}
		seen[value] = true
		result = append(result, value)
	}
	return result
}

func firstPositive(values ...float64) float64 {
	for _, value := range values {
		if value > 0 && !math.IsNaN(value) && !math.IsInf(value, 0) {
			return value
		}
	}
	return 0
}

func minInt64(a, b int64) int64 {
	if a < b {
		return a
	}
	return b
}
