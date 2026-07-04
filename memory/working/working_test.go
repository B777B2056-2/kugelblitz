package working

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestIsValidDAG_Empty(t *testing.T) {
	p := &Plan{SubTasks: nil}
	assert.Error(t, p.Validate())

	p2 := &Plan{SubTasks: []Task{}}
	assert.Error(t, p2.Validate())
}

func TestIsValidDAG_SingleTask_NoDeps(t *testing.T) {
	p := &Plan{SubTasks: []Task{
		{ID: "A", ParentTaskID: ""},
	}}
	assert.NoError(t, p.Validate())
}

func TestIsValidDAG_LinearChain(t *testing.T) {
	p := &Plan{SubTasks: []Task{
		{ID: "A", ParentTaskID: ""},
		{ID: "B", ParentTaskID: "A"},
		{ID: "C", ParentTaskID: "B"},
	}}
	assert.NoError(t, p.Validate())
}

func TestIsValidDAG_Diamond(t *testing.T) {
	p := &Plan{SubTasks: []Task{
		{ID: "A", ParentTaskID: ""},
		{ID: "B", ParentTaskID: ""},
		{ID: "C", ParentTaskID: "A,B"},
		{ID: "D", ParentTaskID: "C"},
	}}
	assert.NoError(t, p.Validate())
}

func TestIsValidDAG_SelfLoop(t *testing.T) {
	p := &Plan{SubTasks: []Task{
		{ID: "A", ParentTaskID: "A"},
	}}
	assert.Error(t, p.Validate())
}

func TestIsValidDAG_TwoNodeCycle(t *testing.T) {
	p := &Plan{SubTasks: []Task{
		{ID: "A", ParentTaskID: "B"},
		{ID: "B", ParentTaskID: "A"},
	}}
	assert.Error(t, p.Validate())
}

func TestIsValidDAG_ThreeNodeCycle(t *testing.T) {
	p := &Plan{SubTasks: []Task{
		{ID: "A", ParentTaskID: "C"},
		{ID: "B", ParentTaskID: "A"},
		{ID: "C", ParentTaskID: "B"},
	}}
	assert.Error(t, p.Validate())
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
	assert.NoError(t, p.Validate())
}

func TestIsValidDAG_DanglingReference(t *testing.T) {
	p := &Plan{SubTasks: []Task{
		{ID: "A", ParentTaskID: "Z"}, // Z does not exist
	}}
	assert.Error(t, p.Validate())
}

func TestIsValidDAG_DanglingRefInMulti(t *testing.T) {
	p := &Plan{SubTasks: []Task{
		{ID: "A", ParentTaskID: ""},
		{ID: "B", ParentTaskID: "A,X"}, // X does not exist
	}}
	assert.Error(t, p.Validate())
}

func TestIsValidDAG_IndependentTasks(t *testing.T) {
	p := &Plan{SubTasks: []Task{
		{ID: "A", ParentTaskID: ""},
		{ID: "B", ParentTaskID: ""},
		{ID: "C", ParentTaskID: ""},
		{ID: "D", ParentTaskID: ""},
	}}
	assert.NoError(t, p.Validate())
}

func TestIsValidDAG_TwoRoots_Linear(t *testing.T) {
	p := &Plan{SubTasks: []Task{
		{ID: "A", ParentTaskID: ""},
		{ID: "B", ParentTaskID: ""},
		{ID: "C", ParentTaskID: "A,B"},
	}}
	assert.NoError(t, p.Validate())
}

func TestIsValidDAG_SelfLoopInMulti(t *testing.T) {
	p := &Plan{SubTasks: []Task{
		{ID: "A", ParentTaskID: ""},
		{ID: "B", ParentTaskID: "A,B"}, // self-reference B
	}}
	assert.Error(t, p.Validate())
}
