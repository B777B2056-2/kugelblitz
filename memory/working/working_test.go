package working

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestIsValidDAG_Empty(t *testing.T) {
	p := &Plan{SubTasks: nil}
	assert.False(t, p.IsValid())

	p2 := &Plan{SubTasks: []Task{}}
	assert.False(t, p2.IsValid())
}

func TestIsValidDAG_SingleTask_NoDeps(t *testing.T) {
	p := &Plan{SubTasks: []Task{
		{ID: "A", ParentTaskID: ""},
	}}
	assert.True(t, p.IsValid())
}

func TestIsValidDAG_LinearChain(t *testing.T) {
	p := &Plan{SubTasks: []Task{
		{ID: "A", ParentTaskID: ""},
		{ID: "B", ParentTaskID: "A"},
		{ID: "C", ParentTaskID: "B"},
	}}
	assert.True(t, p.IsValid())
}

func TestIsValidDAG_Diamond(t *testing.T) {
	p := &Plan{SubTasks: []Task{
		{ID: "A", ParentTaskID: ""},
		{ID: "B", ParentTaskID: ""},
		{ID: "C", ParentTaskID: "A,B"},
		{ID: "D", ParentTaskID: "C"},
	}}
	assert.True(t, p.IsValid())
}

func TestIsValidDAG_SelfLoop(t *testing.T) {
	p := &Plan{SubTasks: []Task{
		{ID: "A", ParentTaskID: "A"},
	}}
	assert.False(t, p.IsValid())
}

func TestIsValidDAG_TwoNodeCycle(t *testing.T) {
	p := &Plan{SubTasks: []Task{
		{ID: "A", ParentTaskID: "B"},
		{ID: "B", ParentTaskID: "A"},
	}}
	assert.False(t, p.IsValid())
}

func TestIsValidDAG_ThreeNodeCycle(t *testing.T) {
	p := &Plan{SubTasks: []Task{
		{ID: "A", ParentTaskID: "C"},
		{ID: "B", ParentTaskID: "A"},
		{ID: "C", ParentTaskID: "B"},
	}}
	assert.False(t, p.IsValid())
}

func TestIsValidDAG_ChainWithCycle(t *testing.T) {
	p := &Plan{SubTasks: []Task{
		{ID: "A", ParentTaskID: ""},
		{ID: "B", ParentTaskID: "A"},
		{ID: "C", ParentTaskID: "B"},
		{ID: "D", ParentTaskID: "C"},
		{ID: "E", ParentTaskID: "D"},
		{ID: "B2", ParentTaskID: "E"}, // references B's position in a cycle path: B → ... → E → B2 but B2 not B
	}}
	// B2 depends on E, E on D, D on C, C on B — no cycle
	assert.True(t, p.IsValid())
}

func TestIsValidDAG_DanglingReference(t *testing.T) {
	p := &Plan{SubTasks: []Task{
		{ID: "A", ParentTaskID: "Z"}, // Z does not exist
	}}
	assert.False(t, p.IsValid())
}

func TestIsValidDAG_DanglingRefInMulti(t *testing.T) {
	p := &Plan{SubTasks: []Task{
		{ID: "A", ParentTaskID: ""},
		{ID: "B", ParentTaskID: "A,X"}, // X does not exist
	}}
	assert.False(t, p.IsValid())
}

func TestIsValidDAG_IndependentTasks(t *testing.T) {
	p := &Plan{SubTasks: []Task{
		{ID: "A", ParentTaskID: ""},
		{ID: "B", ParentTaskID: ""},
		{ID: "C", ParentTaskID: ""},
		{ID: "D", ParentTaskID: ""},
	}}
	assert.True(t, p.IsValid())
}

func TestIsValidDAG_TwoRoots_Linear(t *testing.T) {
	p := &Plan{SubTasks: []Task{
		{ID: "A", ParentTaskID: ""},
		{ID: "B", ParentTaskID: ""},
		{ID: "C", ParentTaskID: "A,B"},
	}}
	assert.True(t, p.IsValid())
}

func TestIsValidDAG_SelfLoopInMulti(t *testing.T) {
	p := &Plan{SubTasks: []Task{
		{ID: "A", ParentTaskID: ""},
		{ID: "B", ParentTaskID: "A,B"}, // self-reference B
	}}
	assert.False(t, p.IsValid())
}
