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

package inst

import (
	"fmt"
	"github.com/outbrain/sqlutils"
	"github.com/outbrain/orchestrator/db"
	"github.com/outbrain/log"
	"github.com/outbrain/orchestrator/config"
)


// AuditOperation creates and writes a new audit entry by given params
func AuditOperation(auditType string, instanceKey *InstanceKey, message string) error {
	db,	err	:=	db.OpenOrchestrator()
	if err != nil {return log.Errore(err)}
	
	if instanceKey == nil {
		instanceKey = &InstanceKey{}
	}
	
	_, err = sqlutils.Exec(db, `
			insert 
				into audit (
					audit_timestamp, audit_type, hostname, port, message
				) VALUES (
					NOW(), ?, ?, ?, ?
				)
			`,
			auditType,
			instanceKey.Hostname, 
		 	instanceKey.Port,
		 	message,
		 )
	if err != nil {return log.Errore(err)}
	
	return err
}

// ReadRecentAudit returns a list of audit entries order chronologically descending, using page number.
func ReadRecentAudit(page int) ([]Audit, error) {
	res := []Audit{}
	query := fmt.Sprintf(`
		select 
			audit_id,
			audit_timestamp,
			audit_type,
			hostname,
			port,
			message
		from 
			audit
		order by
			audit_timestamp desc
		limit %d
		offset %d
		`, config.Config.AuditPageSize, page * config.Config.AuditPageSize)
	db,	err	:=	db.OpenOrchestrator()
    if err != nil {goto Cleanup}
    
    err = sqlutils.QueryRowsMap(db, query, func(m sqlutils.RowMap) error {
    	audit := Audit{}
    	audit.AuditId = m.GetInt64("audit_id")
    	audit.AuditTimestamp = m.GetString("audit_timestamp") 
    	audit.AuditType = m.GetString("audit_type")
    	audit.AuditInstanceKey.Hostname = m.GetString("hostname")
    	audit.AuditInstanceKey.Port = m.GetInt("port")
    	audit.Message = m.GetString("message") 

    	res = append(res, audit)
    	return err       	
   	})
	Cleanup:

	if err	!=	nil	{
		log.Errore(err)
	}
	return res, err

}
