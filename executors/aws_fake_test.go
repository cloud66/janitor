package executors

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/ec2"
	ec2types "github.com/aws/aws-sdk-go-v2/service/ec2/types"
	"github.com/aws/aws-sdk-go-v2/service/elasticloadbalancing"
	elbtypes "github.com/aws/aws-sdk-go-v2/service/elasticloadbalancing/types"
	"github.com/aws/aws-sdk-go-v2/service/elasticloadbalancingv2"
	elbv2types "github.com/aws/aws-sdk-go-v2/service/elasticloadbalancingv2/types"
	"github.com/cloud66/janitor/core"
)

// --- fakes ------------------------------------------------------------------

// callLog captures ordered calls across all fakes.
// tests assert call-order invariants (e.g. the ALB delete ordering B5) by
// inspecting the slice after the operation under test completes.
type callLog struct {
	calls []string
}

func (c *callLog) add(s string) { c.calls = append(c.calls, s) }

// fakeEC2 is a record-and-replay EC2 fake.
// describePages drives pagination for DescribeInstances (one page per element).
type fakeEC2 struct {
	log             *callLog
	describePages   []*ec2.DescribeInstancesOutput
	describeErr     error
	modifyErr       error
	terminateErr    error
}

func (f *fakeEC2) DescribeInstances(ctx context.Context, in *ec2.DescribeInstancesInput, opts ...func(*ec2.Options)) (*ec2.DescribeInstancesOutput, error) {
	f.log.add("ec2.DescribeInstances")
	if f.describeErr != nil {
		return nil, f.describeErr
	}
	// pick the page based on whether input carries a NextToken value.
	// page 0 is returned when NextToken is nil, page N when token="pageN".
	idx := 0
	if in.NextToken != nil {
		fmt.Sscanf(*in.NextToken, "page%d", &idx)
	}
	if idx >= len(f.describePages) {
		return &ec2.DescribeInstancesOutput{}, nil
	}
	return f.describePages[idx], nil
}

func (f *fakeEC2) ModifyInstanceAttribute(ctx context.Context, in *ec2.ModifyInstanceAttributeInput, opts ...func(*ec2.Options)) (*ec2.ModifyInstanceAttributeOutput, error) {
	f.log.add("ec2.ModifyInstanceAttribute")
	return &ec2.ModifyInstanceAttributeOutput{}, f.modifyErr
}

func (f *fakeEC2) TerminateInstances(ctx context.Context, in *ec2.TerminateInstancesInput, opts ...func(*ec2.Options)) (*ec2.TerminateInstancesOutput, error) {
	f.log.add("ec2.TerminateInstances")
	return &ec2.TerminateInstancesOutput{}, f.terminateErr
}

// fakeELB is a record-and-replay classic ELB fake.
type fakeELB struct {
	log         *callLog
	describeErr error
	deleteErr   error
	lbs         []elbtypes.LoadBalancerDescription
}

func (f *fakeELB) DescribeLoadBalancers(ctx context.Context, in *elasticloadbalancing.DescribeLoadBalancersInput, opts ...func(*elasticloadbalancing.Options)) (*elasticloadbalancing.DescribeLoadBalancersOutput, error) {
	f.log.add("elb.DescribeLoadBalancers")
	if f.describeErr != nil {
		return nil, f.describeErr
	}
	return &elasticloadbalancing.DescribeLoadBalancersOutput{LoadBalancerDescriptions: f.lbs}, nil
}

func (f *fakeELB) DeleteLoadBalancer(ctx context.Context, in *elasticloadbalancing.DeleteLoadBalancerInput, opts ...func(*elasticloadbalancing.Options)) (*elasticloadbalancing.DeleteLoadBalancerOutput, error) {
	f.log.add("elb.DeleteLoadBalancer")
	return &elasticloadbalancing.DeleteLoadBalancerOutput{}, f.deleteErr
}

// fakeALB is the biggest fake — most ALB behaviors live here.
// tgPages lets tests drive paginated DescribeTargetGroups (top-level orphan
// scan) via multiple outputs; lbPages does the same for DescribeLoadBalancers.
type fakeALB struct {
	log *callLog

	// paginated outputs for the top-level DescribeLoadBalancers call.
	lbPages []*elasticloadbalancingv2.DescribeLoadBalancersOutput
	lbErr   error

	// paginated outputs for the top-level (no LoadBalancerArn) TG scan.
	tgPages []*elasticloadbalancingv2.DescribeTargetGroupsOutput

	// per-LB describe outputs (keyed by LB ARN). single-page helpers.
	perLBListeners    map[string]*elasticloadbalancingv2.DescribeListenersOutput
	perLBTargetGroups map[string]*elasticloadbalancingv2.DescribeTargetGroupsOutput

	// per-LB paginated outputs — drive multi-page responses in tests.
	// key: "<LB-ARN>|<page-index>" → output. NextMarker in each page steers
	// the caller to "page<N+1>"; absence of a key ends the loop.
	perLBListenerPages    map[string][]*elasticloadbalancingv2.DescribeListenersOutput
	perLBTargetGroupPages map[string][]*elasticloadbalancingv2.DescribeTargetGroupsOutput

	// tags keyed by resource ARN; AddTags/RemoveTags mutate this map.
	tags map[string][]elbv2types.Tag

	// injected errors.
	addTagsErr error

	// health describe — keyed by TG arn.
	health map[string]*elasticloadbalancingv2.DescribeTargetHealthOutput
}

func newFakeALB(log *callLog) *fakeALB {
	return &fakeALB{
		log:                   log,
		perLBListeners:        map[string]*elasticloadbalancingv2.DescribeListenersOutput{},
		perLBTargetGroups:     map[string]*elasticloadbalancingv2.DescribeTargetGroupsOutput{},
		perLBListenerPages:    map[string][]*elasticloadbalancingv2.DescribeListenersOutput{},
		perLBTargetGroupPages: map[string][]*elasticloadbalancingv2.DescribeTargetGroupsOutput{},
		tags:                  map[string][]elbv2types.Tag{},
		health:                map[string]*elasticloadbalancingv2.DescribeTargetHealthOutput{},
	}
}

func (f *fakeALB) DescribeLoadBalancers(ctx context.Context, in *elasticloadbalancingv2.DescribeLoadBalancersInput, opts ...func(*elasticloadbalancingv2.Options)) (*elasticloadbalancingv2.DescribeLoadBalancersOutput, error) {
	f.log.add("alb.DescribeLoadBalancers")
	if f.lbErr != nil {
		return nil, f.lbErr
	}
	// page 0 when Marker nil, else parse index.
	idx := 0
	if in.Marker != nil {
		fmt.Sscanf(*in.Marker, "page%d", &idx)
	}
	if idx >= len(f.lbPages) {
		return &elasticloadbalancingv2.DescribeLoadBalancersOutput{}, nil
	}
	return f.lbPages[idx], nil
}

func (f *fakeALB) DescribeTags(ctx context.Context, in *elasticloadbalancingv2.DescribeTagsInput, opts ...func(*elasticloadbalancingv2.Options)) (*elasticloadbalancingv2.DescribeTagsOutput, error) {
	f.log.add("alb.DescribeTags")
	var descs []elbv2types.TagDescription
	for _, arn := range in.ResourceArns {
		arnCopy := arn
		descs = append(descs, elbv2types.TagDescription{ResourceArn: &arnCopy, Tags: f.tags[arn]})
	}
	return &elasticloadbalancingv2.DescribeTagsOutput{TagDescriptions: descs}, nil
}

func (f *fakeALB) DescribeListeners(ctx context.Context, in *elasticloadbalancingv2.DescribeListenersInput, opts ...func(*elasticloadbalancingv2.Options)) (*elasticloadbalancingv2.DescribeListenersOutput, error) {
	f.log.add("alb.DescribeListeners")
	if in.LoadBalancerArn == nil {
		return &elasticloadbalancingv2.DescribeListenersOutput{}, nil
	}
	// paginated form takes precedence when present for this LB.
	if pages, ok := f.perLBListenerPages[*in.LoadBalancerArn]; ok {
		idx := 0
		if in.Marker != nil {
			fmt.Sscanf(*in.Marker, "page%d", &idx)
		}
		if idx >= len(pages) {
			return &elasticloadbalancingv2.DescribeListenersOutput{}, nil
		}
		return pages[idx], nil
	}
	if out, ok := f.perLBListeners[*in.LoadBalancerArn]; ok {
		return out, nil
	}
	return &elasticloadbalancingv2.DescribeListenersOutput{}, nil
}

func (f *fakeALB) DescribeTargetGroups(ctx context.Context, in *elasticloadbalancingv2.DescribeTargetGroupsInput, opts ...func(*elasticloadbalancingv2.Options)) (*elasticloadbalancingv2.DescribeTargetGroupsOutput, error) {
	f.log.add("alb.DescribeTargetGroups")
	// per-LB query: prefer paginated form, fall back to single-page map.
	if in.LoadBalancerArn != nil {
		if pages, ok := f.perLBTargetGroupPages[*in.LoadBalancerArn]; ok {
			idx := 0
			if in.Marker != nil {
				fmt.Sscanf(*in.Marker, "page%d", &idx)
			}
			if idx >= len(pages) {
				return &elasticloadbalancingv2.DescribeTargetGroupsOutput{}, nil
			}
			return pages[idx], nil
		}
		if out, ok := f.perLBTargetGroups[*in.LoadBalancerArn]; ok {
			return out, nil
		}
		return &elasticloadbalancingv2.DescribeTargetGroupsOutput{}, nil
	}
	// top-level (orphan scan) query — paginate via tgPages.
	idx := 0
	if in.Marker != nil {
		fmt.Sscanf(*in.Marker, "page%d", &idx)
	}
	if idx >= len(f.tgPages) {
		return &elasticloadbalancingv2.DescribeTargetGroupsOutput{}, nil
	}
	return f.tgPages[idx], nil
}

func (f *fakeALB) DescribeTargetHealth(ctx context.Context, in *elasticloadbalancingv2.DescribeTargetHealthInput, opts ...func(*elasticloadbalancingv2.Options)) (*elasticloadbalancingv2.DescribeTargetHealthOutput, error) {
	f.log.add("alb.DescribeTargetHealth")
	if in.TargetGroupArn == nil {
		return &elasticloadbalancingv2.DescribeTargetHealthOutput{}, nil
	}
	if out, ok := f.health[*in.TargetGroupArn]; ok {
		return out, nil
	}
	return &elasticloadbalancingv2.DescribeTargetHealthOutput{}, nil
}

func (f *fakeALB) AddTags(ctx context.Context, in *elasticloadbalancingv2.AddTagsInput, opts ...func(*elasticloadbalancingv2.Options)) (*elasticloadbalancingv2.AddTagsOutput, error) {
	f.log.add("alb.AddTags")
	if f.addTagsErr != nil {
		return nil, f.addTagsErr
	}
	// actually store the tag so later DescribeTags sees it.
	for _, arn := range in.ResourceArns {
		f.tags[arn] = append(f.tags[arn], in.Tags...)
	}
	return &elasticloadbalancingv2.AddTagsOutput{}, nil
}

func (f *fakeALB) RemoveTags(ctx context.Context, in *elasticloadbalancingv2.RemoveTagsInput, opts ...func(*elasticloadbalancingv2.Options)) (*elasticloadbalancingv2.RemoveTagsOutput, error) {
	f.log.add("alb.RemoveTags")
	// remove requested keys from the stored tag list.
	for _, arn := range in.ResourceArns {
		existing := f.tags[arn]
		filtered := existing[:0]
		for _, t := range existing {
			drop := false
			for _, k := range in.TagKeys {
				if t.Key != nil && *t.Key == k {
					drop = true
					break
				}
			}
			if !drop {
				filtered = append(filtered, t)
			}
		}
		f.tags[arn] = filtered
	}
	return &elasticloadbalancingv2.RemoveTagsOutput{}, nil
}

func (f *fakeALB) DeleteListener(ctx context.Context, in *elasticloadbalancingv2.DeleteListenerInput, opts ...func(*elasticloadbalancingv2.Options)) (*elasticloadbalancingv2.DeleteListenerOutput, error) {
	f.log.add("alb.DeleteListener")
	return &elasticloadbalancingv2.DeleteListenerOutput{}, nil
}

func (f *fakeALB) DeleteTargetGroup(ctx context.Context, in *elasticloadbalancingv2.DeleteTargetGroupInput, opts ...func(*elasticloadbalancingv2.Options)) (*elasticloadbalancingv2.DeleteTargetGroupOutput, error) {
	f.log.add("alb.DeleteTargetGroup")
	return &elasticloadbalancingv2.DeleteTargetGroupOutput{}, nil
}

func (f *fakeALB) DeleteLoadBalancer(ctx context.Context, in *elasticloadbalancingv2.DeleteLoadBalancerInput, opts ...func(*elasticloadbalancingv2.Options)) (*elasticloadbalancingv2.DeleteLoadBalancerOutput, error) {
	f.log.add("alb.DeleteLoadBalancer")
	return &elasticloadbalancingv2.DeleteLoadBalancerOutput{}, nil
}

// --- helpers ----------------------------------------------------------------

// captureOut returns a buffer wired into a fresh context under both
// WarnWriterKey and OutWriterKey. tests that want to assert on executor
// output should thread the returned context through their calls.
func captureOut(t *testing.T) (*bytes.Buffer, context.Context) {
	t.Helper()
	buf := &bytes.Buffer{}
	ctx := context.Background()
	ctx = context.WithValue(ctx, core.WarnWriterKey, buf)
	ctx = context.WithValue(ctx, core.OutWriterKey, buf)
	return buf, ctx
}

// newTestAws constructs an Aws executor with single-region scope and the
// provided fakes wired as factories.
func newTestAws(ec2f *fakeEC2, elbf *fakeELB, albf *fakeALB) Aws {
	return Aws{
		regionsOverride: []string{"us-east-1"},
		ec2Factory:      func(ctx context.Context, r string) ec2Client { return ec2f },
		elbFactory:      func(ctx context.Context, r string) elbClient { return elbf },
		albFactory:      func(ctx context.Context, r string) albClient { return albf },
	}
}

// --- P5-T3: ALB delete ordering (B5) ---------------------------------------

func TestAws_LoadBalancerDelete_ALB_Ordering_B5(t *testing.T) {
	log := &callLog{}
	alb := newFakeALB(log)
	a := newTestAws(&fakeEC2{log: log}, &fakeELB{log: log}, alb)

	lb := core.LoadBalancer{
		Type:            "alb",
		Region:          "us-east-1",
		LoadBalancerArn: "arn:lb",
		ListenerArns:    []string{"arn:l1", "arn:l2"},
		TargetGroupArns: []string{"arn:tg1", "arn:tg2"},
	}
	if err := a.LoadBalancerDelete(context.Background(), lb); err != nil {
		t.Fatalf("delete err: %v", err)
	}

	// expected order: listeners → LB → target groups.
	// this is the B5 fix: TGs must be deleted AFTER the LB is gone, otherwise
	// AWS returns "target group is currently in use".
	want := []string{
		"alb.DeleteListener", "alb.DeleteListener",
		"alb.DeleteLoadBalancer",
		"alb.DeleteTargetGroup", "alb.DeleteTargetGroup",
	}
	if !sliceEq(log.calls, want) {
		t.Fatalf("wrong call order:\n got %v\nwant %v", log.calls, want)
	}
}

func sliceEq(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// --- P5-T4: cross-region error aggregation (B6) ----------------------------

func TestAws_LoadBalancersGet_AllRegionsFail_B6(t *testing.T) {
	log := &callLog{}
	elb := &fakeELB{log: log, describeErr: errors.New("AccessDenied")}
	alb := newFakeALB(log)
	alb.lbErr = errors.New("AccessDenied")
	a := newTestAws(&fakeEC2{log: log}, elb, alb)

	_, err := a.LoadBalancersGet(context.Background(), true)
	// B6: before fix this returned (nil, nil). Now must surface an error.
	if err == nil {
		t.Fatal("expected error when all regions fail, got nil")
	}
	if !strings.Contains(err.Error(), "AccessDenied") {
		t.Fatalf("expected error to mention AccessDenied, got %v", err)
	}
}

func TestAws_ServersGet_AllRegionsFail_B6(t *testing.T) {
	log := &callLog{}
	ec2f := &fakeEC2{log: log, describeErr: errors.New("AccessDenied")}
	a := newTestAws(ec2f, &fakeELB{log: log}, newFakeALB(log))

	_, err := a.ServersGet(context.Background(), nil, nil)
	if err == nil {
		t.Fatal("expected error when all regions fail, got nil")
	}
}

// --- P5-T5: AddTags error handling (B7) ------------------------------------

func TestAws_OrphanTG_AddTagsError_B7(t *testing.T) {
	log := &callLog{}
	alb := newFakeALB(log)
	alb.addTagsErr = errors.New("ThrottlingException")
	// orphan TG: no LoadBalancerArns, no existing marked-for-deletion tag.
	tgArn := "arn:tg:orphan"
	alb.tgPages = []*elasticloadbalancingv2.DescribeTargetGroupsOutput{
		{TargetGroups: []elbv2types.TargetGroup{
			{TargetGroupArn: &tgArn, LoadBalancerArns: nil},
		}},
	}
	a := newTestAws(&fakeEC2{log: log}, &fakeELB{log: log}, alb)
	buf, ctx := captureOut(t)

	if _, err := a.LoadBalancersGet(ctx, false); err != nil {
		t.Fatalf("LoadBalancersGet err: %v", err)
	}

	// B7: AddTags failed. We must log a warning and NOT record the tag
	// (so that next run does not falsely treat this TG as "previously marked"
	// and delete it).
	if !strings.Contains(buf.String(), "[WARN] AddTags failed") {
		t.Fatalf("expected WARN line, got: %q", buf.String())
	}
	// fake AddTags returns the error before storing — so tags map stays empty.
	if len(alb.tags[tgArn]) != 0 {
		t.Fatalf("TG must NOT be marked when AddTags fails, got tags: %+v", alb.tags[tgArn])
	}
}

// --- P5-T6: pagination for TG describe (B8) --------------------------------

func TestAws_OrphanTG_PaginationFindsOwningLB_B8(t *testing.T) {
	log := &callLog{}
	alb := newFakeALB(log)
	tgArn := "arn:tg:owned"
	// page 1 is "empty of the TG we care about"; page 2 contains the TG,
	// and that TG has an owning LB. Before the pagination fix the orphan scan
	// only saw page 1 and might have falsely flagged the TG.
	page1Marker := "page1"
	alb.tgPages = []*elasticloadbalancingv2.DescribeTargetGroupsOutput{
		{
			TargetGroups: []elbv2types.TargetGroup{}, // empty page
			NextMarker:   &page1Marker,
		},
		{
			TargetGroups: []elbv2types.TargetGroup{
				{TargetGroupArn: &tgArn, LoadBalancerArns: []string{"arn:some-lb"}},
			},
		},
	}
	// pre-seed tag as though a previous run falsely marked it — the owning-LB
	// branch should REMOVE the tag, not delete the TG.
	key := "JANITOR.MARKED.TO.DELETE"
	val := "true"
	alb.tags[tgArn] = []elbv2types.Tag{{Key: &key, Value: &val}}
	a := newTestAws(&fakeEC2{log: log}, &fakeELB{log: log}, alb)

	if _, err := a.LoadBalancersGet(context.Background(), false); err != nil {
		t.Fatalf("err: %v", err)
	}
	// must have visited page 2 (at least 2 top-level DescribeTargetGroups calls).
	tgCalls := 0
	for _, c := range log.calls {
		if c == "alb.DescribeTargetGroups" {
			tgCalls++
		}
	}
	if tgCalls < 2 {
		t.Fatalf("expected paginated TG describe (≥2 calls), got %d", tgCalls)
	}
	// P5-T8 two-phase race: tag must have been REMOVED (TG re-attached).
	for _, c := range log.calls {
		if c == "alb.DeleteTargetGroup" {
			t.Fatal("TG with owning LB must NOT be deleted")
		}
	}
	if len(alb.tags[tgArn]) != 0 {
		t.Fatalf("tag should be removed on re-attached TG, got %+v", alb.tags[tgArn])
	}
}

// --- P5-T6 additionally: paginated DescribeInstances pagination -----------

func TestAws_ServersGet_Pagination_B8(t *testing.T) {
	log := &callLog{}
	now := time.Now().Add(-48 * time.Hour)
	idA := "i-a"
	idB := "i-b"
	page1Next := "page1"
	ec2f := &fakeEC2{
		log: log,
		describePages: []*ec2.DescribeInstancesOutput{
			{
				Reservations: []ec2types.Reservation{{Instances: []ec2types.Instance{
					{
						InstanceId:          &idA,
						State:               &ec2types.InstanceState{Name: "running"},
						BlockDeviceMappings: []ec2types.InstanceBlockDeviceMapping{{Ebs: &ec2types.EbsInstanceBlockDevice{AttachTime: &now}}},
					},
				}}},
				NextToken: &page1Next,
			},
			{
				Reservations: []ec2types.Reservation{{Instances: []ec2types.Instance{
					{
						InstanceId:          &idB,
						State:               &ec2types.InstanceState{Name: "running"},
						BlockDeviceMappings: []ec2types.InstanceBlockDeviceMapping{{Ebs: &ec2types.EbsInstanceBlockDevice{AttachTime: &now}}},
					},
				}}},
			},
		},
	}
	a := newTestAws(ec2f, &fakeELB{log: log}, newFakeALB(log))

	servers, err := a.ServersGet(context.Background(), nil, nil)
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if len(servers) != 2 {
		t.Fatalf("expected 2 servers across 2 pages, got %d", len(servers))
	}
}

// --- P5-T7: nil-pointer guards ---------------------------------------------

func TestAws_ServersGet_NilEbs_SkipsWithWarning(t *testing.T) {
	log := &callLog{}
	id1 := "i-nil"
	id2 := "i-ok"
	now := time.Now().Add(-24 * time.Hour)
	ec2f := &fakeEC2{
		log: log,
		describePages: []*ec2.DescribeInstancesOutput{
			{
				Reservations: []ec2types.Reservation{{Instances: []ec2types.Instance{
					// nil Ebs — old code would panic here.
					{
						InstanceId:          &id1,
						State:               &ec2types.InstanceState{Name: "running"},
						BlockDeviceMappings: []ec2types.InstanceBlockDeviceMapping{{Ebs: nil}},
					},
					// normal instance still gets included.
					{
						InstanceId:          &id2,
						State:               &ec2types.InstanceState{Name: "running"},
						BlockDeviceMappings: []ec2types.InstanceBlockDeviceMapping{{Ebs: &ec2types.EbsInstanceBlockDevice{AttachTime: &now}}},
					},
				}}},
			},
		},
	}
	a := newTestAws(ec2f, &fakeELB{log: log}, newFakeALB(log))
	buf, ctx := captureOut(t)

	servers, err := a.ServersGet(ctx, nil, nil)
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if len(servers) != 1 {
		t.Fatalf("expected only the well-formed instance, got %d", len(servers))
	}
	if !strings.Contains(buf.String(), "nil Ebs/AttachTime") {
		t.Fatalf("expected WARN for nil Ebs, got %q", buf.String())
	}
}

// --- sanity: ServerDelete happy path wires terminate -----------------------

func TestAws_ServerDelete_CallsModifyThenTerminate(t *testing.T) {
	log := &callLog{}
	a := newTestAws(&fakeEC2{log: log}, &fakeELB{log: log}, newFakeALB(log))
	err := a.ServerDelete(context.Background(), core.Server{VendorID: "i-1", Region: "us-east-1"})
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	want := []string{"ec2.ModifyInstanceAttribute", "ec2.TerminateInstances"}
	if !sliceEq(log.calls, want) {
		t.Fatalf("wrong call order %v", log.calls)
	}
}

// dummy use of aws.String to stop unused-import when edits churn.
var _ = aws.String

// --- followup: partial-failure per-region granularity ---------------------

// TestAws_LoadBalancersGet_ELBFailALBSuccess asserts that a region where the
// classic ELB call errors but the ALB call succeeds is NOT counted as "failed"
// — the old code flipped regionFailed on any single-service error, which
// propagated to "all regions failed" aggregation downstream.
func TestAws_LoadBalancersGet_ELBFailALBSuccess(t *testing.T) {
	log := &callLog{}
	elb := &fakeELB{log: log, describeErr: errors.New("AccessDenied")}
	alb := newFakeALB(log)
	// one healthy ALB with no target groups (so InstanceCount=0).
	lbArn := "arn:alb:1"
	lbName := "alb1"
	created := time.Now().Add(-48 * time.Hour)
	alb.lbPages = []*elasticloadbalancingv2.DescribeLoadBalancersOutput{
		{LoadBalancers: []elbv2types.LoadBalancer{
			{LoadBalancerArn: &lbArn, LoadBalancerName: &lbName, CreatedTime: &created},
		}},
	}
	a := newTestAws(&fakeEC2{log: log}, elb, alb)

	lbs, err := a.LoadBalancersGet(context.Background(), true)
	if err != nil {
		t.Fatalf("unexpected error — ELB-fail+ALB-success must count as region OK: %v", err)
	}
	if len(lbs) != 1 {
		t.Fatalf("expected ALB to be returned despite ELB failure, got %d", len(lbs))
	}
}

// --- followup: dead TERMINATED filter (real EC2 state mapping) ------------

// TestAws_ELB_ExcludesTerminatedMembers seeds an ELB with 3 members — 2
// running and 1 terminated — and asserts InstanceCount is 2, not 3. Before
// the fix, ServersGet stamped every returned server as "RUNNING", so the
// downstream `!= "TERMINATED"` filter was dead and the count was wrong.
func TestAws_ELB_ExcludesTerminatedMembers(t *testing.T) {
	log := &callLog{}
	now := time.Now().Add(-48 * time.Hour)
	idA, idB, idC := "i-a", "i-b", "i-c"
	ec2f := &fakeEC2{
		log: log,
		describePages: []*ec2.DescribeInstancesOutput{{
			Reservations: []ec2types.Reservation{{Instances: []ec2types.Instance{
				{InstanceId: &idA, State: &ec2types.InstanceState{Name: "running"},
					BlockDeviceMappings: []ec2types.InstanceBlockDeviceMapping{{Ebs: &ec2types.EbsInstanceBlockDevice{AttachTime: &now}}}},
				{InstanceId: &idB, State: &ec2types.InstanceState{Name: "running"},
					BlockDeviceMappings: []ec2types.InstanceBlockDeviceMapping{{Ebs: &ec2types.EbsInstanceBlockDevice{AttachTime: &now}}}},
				{InstanceId: &idC, State: &ec2types.InstanceState{Name: "terminated"},
					BlockDeviceMappings: []ec2types.InstanceBlockDeviceMapping{{Ebs: &ec2types.EbsInstanceBlockDevice{AttachTime: &now}}}},
			}}},
		}},
	}
	// classic ELB referencing all three members.
	lbName := "elb-with-dead-member"
	lbCreated := time.Now().Add(-96 * time.Hour)
	elb := &fakeELB{log: log, lbs: []elbtypes.LoadBalancerDescription{{
		LoadBalancerName: &lbName,
		CreatedTime:      &lbCreated,
		Instances: []elbtypes.Instance{
			{InstanceId: &idA}, {InstanceId: &idB}, {InstanceId: &idC},
		},
	}}}
	a := newTestAws(ec2f, elb, newFakeALB(log))

	lbs, err := a.LoadBalancersGet(context.Background(), true)
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	// find the classic ELB entry — there will also be any ALB results (none here).
	var got *core.LoadBalancer
	for i := range lbs {
		if lbs[i].Type == "elb" {
			got = &lbs[i]
			break
		}
	}
	if got == nil {
		t.Fatal("expected a classic ELB result")
	}
	if got.InstanceCount != 2 {
		t.Fatalf("expected InstanceCount=2 (terminated excluded), got %d", got.InstanceCount)
	}
}

// --- followup: DescribeListeners pagination ------------------------------

func TestAws_ALB_ListenersPagination(t *testing.T) {
	log := &callLog{}
	alb := newFakeALB(log)
	lbArn := "arn:alb:p"
	lbName := "alb-p"
	created := time.Now().Add(-48 * time.Hour)
	alb.lbPages = []*elasticloadbalancingv2.DescribeLoadBalancersOutput{
		{LoadBalancers: []elbv2types.LoadBalancer{
			{LoadBalancerArn: &lbArn, LoadBalancerName: &lbName, CreatedTime: &created},
		}},
	}
	// two-page listener response.
	l1, l2, l3 := "arn:l1", "arn:l2", "arn:l3"
	p1Marker := "page1"
	alb.perLBListenerPages[lbArn] = []*elasticloadbalancingv2.DescribeListenersOutput{
		{Listeners: []elbv2types.Listener{{ListenerArn: &l1}, {ListenerArn: &l2}}, NextMarker: &p1Marker},
		{Listeners: []elbv2types.Listener{{ListenerArn: &l3}}},
	}
	a := newTestAws(&fakeEC2{log: log}, &fakeELB{log: log}, alb)

	lbs, err := a.LoadBalancersGet(context.Background(), true)
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	var got *core.LoadBalancer
	for i := range lbs {
		if lbs[i].Type == "alb" {
			got = &lbs[i]
		}
	}
	if got == nil || len(got.ListenerArns) != 3 {
		t.Fatalf("expected 3 listeners across 2 pages, got %+v", got)
	}
}

// --- followup: per-LB DescribeTargetGroups pagination --------------------

func TestAws_ALB_PerLBTargetGroupsPagination(t *testing.T) {
	log := &callLog{}
	alb := newFakeALB(log)
	lbArn := "arn:alb:tgp"
	lbName := "alb-tgp"
	created := time.Now().Add(-48 * time.Hour)
	alb.lbPages = []*elasticloadbalancingv2.DescribeLoadBalancersOutput{
		{LoadBalancers: []elbv2types.LoadBalancer{
			{LoadBalancerArn: &lbArn, LoadBalancerName: &lbName, CreatedTime: &created},
		}},
	}
	tg1, tg2, tg3 := "arn:tg1", "arn:tg2", "arn:tg3"
	p1Marker := "page1"
	alb.perLBTargetGroupPages[lbArn] = []*elasticloadbalancingv2.DescribeTargetGroupsOutput{
		{TargetGroups: []elbv2types.TargetGroup{{TargetGroupArn: &tg1}, {TargetGroupArn: &tg2}}, NextMarker: &p1Marker},
		{TargetGroups: []elbv2types.TargetGroup{{TargetGroupArn: &tg3}}},
	}
	a := newTestAws(&fakeEC2{log: log}, &fakeELB{log: log}, alb)

	lbs, err := a.LoadBalancersGet(context.Background(), true)
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	var got *core.LoadBalancer
	for i := range lbs {
		if lbs[i].Type == "alb" {
			got = &lbs[i]
		}
	}
	if got == nil || len(got.TargetGroupArns) != 3 {
		t.Fatalf("expected 3 TGs across 2 pages, got %+v", got)
	}
}
