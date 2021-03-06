/*
   Copyright 2014 Outbrain Inc.

   Licensed under the Apache License, Version 2.0 (the "License");
   you may not use this file except in compliance with the License.
   You may obtain a copy of the License at

       http://www.apache.org/licenses/LICENSE-2.0

   Unless required by applicable law or agreed to in writing, software
   distributed under the License is distributed on an "AS IS" BASIS,
   WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
   See the License for the specific language governing permissions and
   limitations under the License.
*/

package http

import (
	"net/http"	
	"fmt"
	"strconv"	
	"encoding/json"
	"github.com/go-martini/martini"
	"github.com/martini-contrib/render"

	"github.com/outbrain/orchestrator/inst"
	"github.com/outbrain/orchestrator/logic"
)

type HttpAPI struct{}

var API HttpAPI = HttpAPI{}


// APIResponseCode is an OK/ERROR response code
type APIResponseCode int

const (
	ERROR APIResponseCode = iota
	OK
)

func (this *APIResponseCode) MarshalJSON() ([]byte, error) {
	return json.Marshal(this.String())
}

func (this *APIResponseCode) String() string {
	switch *this {
		case ERROR: return "ERROR"
		case OK: return "OK"
	}
	return "unknown"
}


// APIResponse is a response returned as JSON to various requests.
type APIResponse struct {
	Code	APIResponseCode
	Message	string
	Details	interface{}
}


func (this *HttpAPI) getInstanceKey(host string, port string) (inst.InstanceKey, error) {
	instanceKey, err := inst.NewInstanceKeyFromStrings(host, port)
	return *instanceKey, err
}

// Instance reads and returns an instance's details.
func (this *HttpAPI) Instance(params martini.Params, r render.Render) {
	instanceKey, err := this.getInstanceKey(params["host"], params["port"])

	if err != nil {
		r.JSON(200, &APIResponse{Code:ERROR, Message: err.Error(),})
		return
	}
	instance, found, err := inst.ReadInstance(&instanceKey)
	if (!found) || (err != nil) {
		r.JSON(200, &APIResponse{Code:ERROR, Message: fmt.Sprintf("Cannot read instance: %+v", instanceKey),})
		return
	}
	r.JSON(200, instance)
}

// Discover starts an asynchronuous discovery for an instance
func (this *HttpAPI) Discover(params martini.Params, r render.Render) {
	instanceKey, err := this.getInstanceKey(params["host"], params["port"])

	if err != nil {
		r.JSON(200, &APIResponse{Code:ERROR, Message: err.Error(),})
		return
	}
	go orchestrator.StartDiscovery(instanceKey)

	r.JSON(200, &APIResponse{Code:OK, Message: fmt.Sprintf("Instance submitted for discovery: %+v", instanceKey),})
}

// Refresh synchronuously re-reads a topology instance
func (this *HttpAPI) Refresh(params martini.Params, r render.Render) {
	instanceKey, err := this.getInstanceKey(params["host"], params["port"])

	if err != nil {
		r.JSON(200, &APIResponse{Code:ERROR, Message: err.Error(),})
		return
	}
	
	_, err = inst.RefreshTopologyInstance(&instanceKey)
	if err != nil {
		r.JSON(200, &APIResponse{Code:ERROR, Message: err.Error(),})
		return
	}
	
	r.JSON(200, &APIResponse{Code:OK, Message: fmt.Sprintf("Instance refreshedh: %+v", instanceKey),})
}


// Forget removes an instance entry fro backend database
func (this *HttpAPI) Forget(params martini.Params, r render.Render) {
	instanceKey, err := this.getInstanceKey(params["host"], params["port"])

	if err != nil {
		r.JSON(200, &APIResponse{Code:ERROR, Message: err.Error(),})
		return
	}
	inst.ForgetInstance(&instanceKey)

	r.JSON(200, &APIResponse{Code:OK, Message: fmt.Sprintf("Instance forgotten: %+v", instanceKey),})
}


// BeginMaintenance begins maintenance mode for given instance
func (this *HttpAPI) BeginMaintenance(params martini.Params, r render.Render) {
	instanceKey, err := this.getInstanceKey(params["host"], params["port"])

	if err != nil {
		r.JSON(200, &APIResponse{Code:ERROR, Message: err.Error(),})
		return
	}
	key, err := inst.BeginMaintenance(&instanceKey, params["owner"], params["reason"])
	if err != nil {
		r.JSON(200, &APIResponse{Code:ERROR, Message: err.Error(), Details: key,})
		return
	}

	r.JSON(200, &APIResponse{Code:OK, Message: fmt.Sprintf("Maintenance begun: %+v", instanceKey),})
}


// EndMaintenance terminates maintenance mode
func (this *HttpAPI) EndMaintenance(params martini.Params, r render.Render) {
	maintenanceKey, err := strconv.ParseInt(params["maintenanceKey"], 10, 0)
	if err != nil {
		r.JSON(200, &APIResponse{Code:ERROR, Message: err.Error(),})
		return
	}
	err = inst.EndMaintenance(maintenanceKey)
	if err != nil {
		r.JSON(200, &APIResponse{Code:ERROR, Message: err.Error(),})
		return
	}

	r.JSON(200, &APIResponse{Code:OK, Message: fmt.Sprintf("Maintenance ended: %+v", maintenanceKey),})
}


// EndMaintenanceByInstanceKey terminates maintenance mode for given instance
func (this *HttpAPI) EndMaintenanceByInstanceKey(params martini.Params, r render.Render) {
	instanceKey, err := this.getInstanceKey(params["host"], params["port"])

	if err != nil {
		r.JSON(200, &APIResponse{Code:ERROR, Message: err.Error(),})
		return
	}
	err = inst.EndMaintenanceByInstanceKey(&instanceKey)
	if err != nil {
		r.JSON(200, &APIResponse{Code:ERROR, Message: err.Error(),})
		return
	}

	r.JSON(200, &APIResponse{Code:OK, Message: fmt.Sprintf("Maintenance ended: %+v", instanceKey),})
}


// Maintenance provides list of instance under active maintenance
func (this *HttpAPI) Maintenance(params martini.Params, r render.Render) {
	instanceKeys, err := inst.ReadActiveMaintenance()
							  
	if err != nil {
		r.JSON(200, &APIResponse{Code:ERROR, Message: fmt.Sprintf("%+v", err),})
		return
	}

	r.JSON(200, instanceKeys)
}


// MoveUp attempts to move an instance up the topology
func (this *HttpAPI) MoveUp(params martini.Params, r render.Render) {
	instanceKey, err := this.getInstanceKey(params["host"], params["port"])

	if err != nil {
		r.JSON(200, &APIResponse{Code:ERROR, Message: err.Error(),})
		return
	}
	instance, err := inst.MoveUp(&instanceKey)
	if err != nil {
		r.JSON(200, &APIResponse{Code:ERROR, Message: err.Error(),})
		return
	}

	r.JSON(200, &APIResponse{Code:OK, Message: "Instance moved up", Details: instance})
}


// MoveUp attempts to move an instance below its supposed sibling
func (this *HttpAPI) MoveBelow(params martini.Params, r render.Render) {
	instanceKey, err := this.getInstanceKey(params["host"], params["port"])
	if err != nil {
		r.JSON(200, &APIResponse{Code:ERROR, Message: err.Error(),})
		return
	}
	siblingKey, err := this.getInstanceKey(params["siblingHost"], params["siblingPort"])
	if err != nil {
		r.JSON(200, &APIResponse{Code:ERROR, Message: err.Error(),})
		return
	}
	
	instance, err := inst.MoveBelow(&instanceKey, &siblingKey)
	if err != nil {
		r.JSON(200, &APIResponse{Code:ERROR, Message: err.Error(),})
		return
	}

	r.JSON(200, &APIResponse{Code:OK, Message: fmt.Sprintf("Instance %+v moved below %+v", instanceKey, siblingKey), Details: instance})
}


// StartSlave starts replication on given instance
func (this *HttpAPI) StartSlave(params martini.Params, r render.Render) {
	instanceKey, err := this.getInstanceKey(params["host"], params["port"])

	if err != nil {
		r.JSON(200, &APIResponse{Code:ERROR, Message: err.Error(),})
		return
	}
	instance, err := inst.StartSlave(&instanceKey)
	if err != nil {
		r.JSON(200, &APIResponse{Code:ERROR, Message: err.Error(),})
		return
	}

	r.JSON(200, &APIResponse{Code:OK, Message: "Slave started", Details: instance})
}


// StartSlave stops replication on given instance
func (this *HttpAPI) StopSlave(params martini.Params, r render.Render) {
	instanceKey, err := this.getInstanceKey(params["host"], params["port"])

	if err != nil {
		r.JSON(200, &APIResponse{Code:ERROR, Message: err.Error(),})
		return
	}
	instance, err := inst.StopSlave(&instanceKey)
	if err != nil {
		r.JSON(200, &APIResponse{Code:ERROR, Message: err.Error(),})
		return
	}

	r.JSON(200, &APIResponse{Code:OK, Message: "Slave stopped", Details: instance})
}


// Cluster provides list of instances in given cluster
func (this *HttpAPI) Cluster(params martini.Params, r render.Render) {
	instances, err := inst.ReadClusterInstances(params["clusterName"])

	if err != nil {
		r.JSON(200, &APIResponse{Code:ERROR, Message: fmt.Sprintf("%+v", err),})
		return
	}

	r.JSON(200, instances)
}


// Clusters provides list of known clusters
func (this *HttpAPI) Clusters(params martini.Params, r render.Render) {
	clusterNames, err := inst.ReadClusters()

	if err != nil {
		r.JSON(200, &APIResponse{Code:ERROR, Message: fmt.Sprintf("%+v", err),})
		return
	}

	r.JSON(200, clusterNames)
}


// Search provides list of instances matching given search param via various criteria.
func (this *HttpAPI) Search(params martini.Params, r render.Render, req *http.Request) {
	searchString := params["searchString"]
	if searchString == "" {
		searchString = req.URL.Query().Get("s");
	}
	instances, err := inst.SearchInstances(searchString)

	if err != nil {
		r.JSON(200, &APIResponse{Code:ERROR, Message: fmt.Sprintf("%+v", err),})
		return
	}

	r.JSON(200, instances)
}


// Problems provides list of instances with known problems
func (this *HttpAPI) Problems(params martini.Params, r render.Render, req *http.Request) {
	instances, err := inst.ReadProblemInstances()

	if err != nil {
		r.JSON(200, &APIResponse{Code:ERROR, Message: fmt.Sprintf("%+v", err),})
		return
	}

	r.JSON(200, instances)
}


// Audit provides listof audit entries by given page number
func (this *HttpAPI) Audit(params martini.Params, r render.Render, req *http.Request) {
	page, err := strconv.Atoi(params["page"])
	if err != nil || page < 0 { page = 0 }
	audits, err := inst.ReadRecentAudit(page)

	if err != nil {
		r.JSON(200, &APIResponse{Code:ERROR, Message: fmt.Sprintf("%+v", err),})
		return
	}

	r.JSON(200, audits)
}


// RegisterRequests makes for the de-facto list of known API calls
func (this *HttpAPI) RegisterRequests(m *martini.ClassicMartini) {
	m.Get("/api/instance/:host/:port", this.Instance) 
	m.Get("/api/discover/:host/:port", this.Discover) 
	m.Get("/api/refresh/:host/:port", this.Refresh) 
	m.Get("/api/forget/:host/:port", this.Forget) 
	m.Get("/api/move-up/:host/:port", this.MoveUp) 
	m.Get("/api/move-below/:host/:port/:siblingHost/:siblingPort", this.MoveBelow) 
	m.Get("/api/begin-maintenance/:host/:port/:owner/:reason", this.BeginMaintenance) 
	m.Get("/api/end-maintenance/:host/:port", this.EndMaintenanceByInstanceKey) 
	m.Get("/api/end-maintenance/:maintenanceKey", this.EndMaintenance)	
	m.Get("/api/start-slave/:host/:port", this.StartSlave) 
	m.Get("/api/stop-slave/:host/:port", this.StopSlave) 
	m.Get("/api/maintenance", this.Maintenance) 
	m.Get("/api/cluster/:clusterName", this.Cluster) 
	m.Get("/api/clusters", this.Clusters) 
	m.Get("/api/search/:searchString", this.Search) 
	m.Get("/api/search", this.Search) 
	m.Get("/api/problems", this.Problems) 
	m.Get("/api/audit", this.Audit) 
	m.Get("/api/audit/:page", this.Audit) 
}
