-- Keep Channel failure alerts as the closed forge_alert payload defined by
-- storage.md §6.6. Other forge_alert producers retain their existing contracts.
CREATE TRIGGER IF NOT EXISTS channel_failure_alert_closed_payload
BEFORE INSERT ON outbox_operations
WHEN NEW.kind='forge_alert' AND NEW.operation_key LIKE 'alert:channel_failure:%'
 AND (
   json_valid(NEW.payload_json)=0
   OR (SELECT count(*) FROM json_each(NEW.payload_json)) <> 7
   OR json_type(NEW.payload_json,'$.forge_host') <> 'text'
   OR json_type(NEW.payload_json,'$.forge_kind') <> 'text'
   OR json_type(NEW.payload_json,'$.forge_project_key') <> 'text'
   OR json_type(NEW.payload_json,'$.markdown') <> 'text'
   OR json_type(NEW.payload_json,'$.purpose') <> 'text'
   OR json_type(NEW.payload_json,'$.target_id') <> 'text'
   OR json_type(NEW.payload_json,'$.target_kind') <> 'text'
   OR json_extract(NEW.payload_json,'$.purpose') <> 'channel_failure'
   OR json_extract(NEW.payload_json,'$.forge_host') = ''
   OR json_extract(NEW.payload_json,'$.forge_kind') = ''
   OR json_extract(NEW.payload_json,'$.forge_project_key') = ''
   OR json_extract(NEW.payload_json,'$.markdown') = ''
   OR json_extract(NEW.payload_json,'$.target_id') = ''
   OR json_extract(NEW.payload_json,'$.target_kind') = ''
 )
BEGIN SELECT RAISE(ABORT,'invalid closed channel failure alert'); END;
