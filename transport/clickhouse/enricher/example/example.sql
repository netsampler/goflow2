ALTER TABLE flows.flows_local ON CLUSTER ... ADD COLUMN dst_service String;
ALTER TABLE flows.flows ON CLUSTER ... ADD COLUMN dst_service String;
