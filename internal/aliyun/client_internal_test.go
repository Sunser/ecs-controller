package aliyun

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"reflect"
	"strings"
	"testing"
	"time"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}

func TestDescribeInstancesQueryRequestsPrimaryENIIPv6(t *testing.T) {
	query := describeInstancesQuery("cn-hangzhou", 2)

	if query["RegionId"] != "cn-hangzhou" {
		t.Fatalf("RegionId = %q, want cn-hangzhou", query["RegionId"])
	}
	if query["PageNumber"] != "2" {
		t.Fatalf("PageNumber = %q, want 2", query["PageNumber"])
	}
	if query["AdditionalAttributes.1"] != "NETWORK_PRIMARY_ENI_IP" {
		t.Fatalf("AdditionalAttributes.1 = %q, want NETWORK_PRIMARY_ENI_IP", query["AdditionalAttributes.1"])
	}
}

func TestRawIPv6SetsAddressesSupportsBothResponseShapes(t *testing.T) {
	set := rawIPv6Sets{
		IPv6Set: []rawIPv6Set{
			{IPv6Address: "2408:4001::1"},
			{IPv6Address: "2408:4001::2"},
		},
		IPv6Address: []string{"2408:4001::2", "2408:4001::3"},
	}

	got := set.addresses()
	want := []string{"2408:4001::2", "2408:4001::3", "2408:4001::1"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("addresses = %#v, want %#v", got, want)
	}
}

func TestRawInstanceToInstanceMergesIPv6Sources(t *testing.T) {
	raw := rawInstance{
		InstanceID:              "i-test",
		Status:                  "Running",
		InstanceType:            "ecs.t6-c1m1.large",
		InstanceChargeType:      "PostPaid",
		CPU:                     2,
		Memory:                  2048,
		InternetMaxBandwidthIn:  200,
		InternetMaxBandwidthOut: 5,
	}
	raw.EIPAddress.Bandwidth = 30
	raw.IPv6Address.IPv6Address = []string{"2408:4001::1"}
	raw.VPCAttributes.IPv6Address.IPv6Address = []string{"2408:4001::2"}
	raw.NetworkInterfaces.NetworkInterface = []rawNetworkInterface{
		{
			IPv6Sets: rawIPv6Sets{
				IPv6Set: []rawIPv6Set{
					{IPv6Address: "2408:4001::3"},
				},
				IPv6Address: []string{"2408:4001::4"},
			},
		},
	}

	instance := raw.toInstance(Region{RegionID: "cn-hangzhou", LocalName: "华东 1"})
	want := []string{"2408:4001::1", "2408:4001::2", "2408:4001::4", "2408:4001::3"}
	if !reflect.DeepEqual(instance.IPv6Addresses, want) {
		t.Fatalf("IPv6Addresses = %#v, want %#v", instance.IPv6Addresses, want)
	}
	if instance.InstanceType != "ecs.t6-c1m1.large" {
		t.Fatalf("InstanceType = %q", instance.InstanceType)
	}
	if instance.InstanceChargeType != "PostPaid" {
		t.Fatalf("InstanceChargeType = %q", instance.InstanceChargeType)
	}
	if instance.CPU != 2 || instance.Memory != 2048 {
		t.Fatalf("CPU/Memory = %d/%d, want 2/2048", instance.CPU, instance.Memory)
	}
	if instance.InternetMaxBandwidthIn != 200 {
		t.Fatalf("InternetMaxBandwidthIn = %d, want 200", instance.InternetMaxBandwidthIn)
	}
	if instance.InternetMaxBandwidthOut != 30 {
		t.Fatalf("InternetMaxBandwidthOut = %d, want EIP bandwidth 30", instance.InternetMaxBandwidthOut)
	}
}

func TestStopInstanceIncludesStoppedMode(t *testing.T) {
	client := NewClient("ak", "sk", SiteChina, 0)
	var stoppedMode string
	client.httpClient = &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		query := req.URL.Query()
		stoppedMode = query.Get("StoppedMode")
		if query.Get("Action") != "StopInstance" {
			t.Fatalf("Action = %q, want StopInstance", query.Get("Action"))
		}
		if query.Get("InstanceId") != "i-test" {
			t.Fatalf("InstanceId = %q, want i-test", query.Get("InstanceId"))
		}
		return &http.Response{
			StatusCode: http.StatusOK,
			Body:       io.NopCloser(strings.NewReader(`{}`)),
			Header:     make(http.Header),
		}, nil
	})}

	if err := client.StopInstance(context.Background(), "cn-hangzhou", "i-test", StoppedModeStopCharging); err != nil {
		t.Fatalf("StopInstance() error = %v", err)
	}
	if stoppedMode != StoppedModeStopCharging {
		t.Fatalf("StoppedMode = %q, want %s", stoppedMode, StoppedModeStopCharging)
	}
}

func TestCDTScopeForRegionSeparatesMainlandAndNonMainland(t *testing.T) {
	cases := map[string]string{
		"cn-hangzhou":    CDTScopeMainland,
		"cn-beijing":     CDTScopeMainland,
		"cn-hongkong":    CDTScopeOverseas,
		"ap-southeast-1": CDTScopeOverseas,
	}

	for regionID, want := range cases {
		if got := CDTScopeForRegion(regionID); got != want {
			t.Fatalf("CDTScopeForRegion(%q) = %q, want %q", regionID, got, want)
		}
	}
}

func TestSplitCDTTrafficGBSeparatesFreeQuotaPools(t *testing.T) {
	gb := 1024.0 * 1024 * 1024
	total, mainland, overseas, regions := splitCDTTrafficGB([]rawCDTTrafficDetail{
		{BusinessRegionID: "cn-hangzhou", Traffic: 3 * gb},
		{BusinessRegionID: "cn-hongkong", Traffic: 5 * gb},
		{BusinessRegionID: "ap-southeast-1", Traffic: 7 * gb},
		{BusinessRegionID: "ap-southeast-1", Traffic: 2 * gb},
	})

	if total != 17 || mainland != 3 || overseas != 14 {
		t.Fatalf("total/mainland/overseas = %.0f/%.0f/%.0f, want 17/3/14", total, mainland, overseas)
	}
	if len(regions) != 3 {
		t.Fatalf("len(regions) = %d, want 3: %#v", len(regions), regions)
	}
	if regions[0].RegionID != "ap-southeast-1" || regions[0].GB != 9 || regions[0].Scope != CDTScopeOverseas {
		t.Fatalf("first region = %#v, want ap-southeast-1 9 overseas", regions[0])
	}
	if regions[1].RegionID != "cn-hongkong" || regions[1].GB != 5 {
		t.Fatalf("second region = %#v, want cn-hongkong 5", regions[1])
	}
	if regions[2].RegionID != "cn-hangzhou" || regions[2].GB != 3 || regions[2].Scope != CDTScopeMainland {
		t.Fatalf("third region = %#v, want cn-hangzhou 3 mainland", regions[2])
	}
}

func TestInstanceOutboundTrafficQueriesVPCMetricWithoutCurrentPublicIP(t *testing.T) {
	client := NewClient("ak", "sk", SiteChina, 0)
	var firstMetricName string
	var firstDimensions []map[string]string
	client.httpClient = &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		query := req.URL.Query()
		if query.Get("Action") != "DescribeMetricList" {
			t.Fatalf("Action = %q, want DescribeMetricList", query.Get("Action"))
		}
		if firstMetricName == "" {
			firstMetricName = query.Get("MetricName")
			if err := json.Unmarshal([]byte(query.Get("Dimensions")), &firstDimensions); err != nil {
				t.Fatalf("Dimensions unmarshal error = %v; raw=%q", err, query.Get("Dimensions"))
			}
		}
		return &http.Response{
			StatusCode: http.StatusOK,
			Body:       io.NopCloser(strings.NewReader(`{"Datapoints":"[]","NextToken":""}`)),
			Header:     make(http.Header),
		}, nil
	})}

	_, err := client.InstanceOutboundTrafficGB(
		context.Background(),
		Instance{InstanceID: "i-stopped", RegionID: "cn-hangzhou", Status: "Stopped"},
		time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC),
		time.Date(2026, 5, 2, 0, 0, 0, 0, time.UTC),
	)
	if err != nil {
		t.Fatalf("InstanceOutboundTrafficGB() error = %v", err)
	}
	if firstMetricName != "VPC_PublicIP_InternetOutRate" {
		t.Fatalf("first MetricName = %q, want VPC_PublicIP_InternetOutRate", firstMetricName)
	}
	if len(firstDimensions) != 1 || firstDimensions[0]["instanceId"] != "i-stopped" || firstDimensions[0]["ip"] != "" {
		t.Fatalf("first Dimensions = %#v, want instanceId only", firstDimensions)
	}
}
