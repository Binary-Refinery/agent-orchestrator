-- +goose Up
CREATE TABLE instructor_inventory_cache (
 id INTEGER PRIMARY KEY CHECK(id=1),
 body TEXT NOT NULL CHECK(json_valid(body))
);
-- +goose StatementBegin
CREATE TRIGGER instructor_inventory_insert AFTER INSERT ON instructor_inventory_cache BEGIN
 INSERT INTO change_log(project_id,session_id,event_type,payload) VALUES(NULL,NULL,'session_updated',json_object('instructorInventory',1));
END;
-- +goose StatementEnd
-- +goose StatementBegin
CREATE TRIGGER instructor_inventory_update AFTER UPDATE ON instructor_inventory_cache BEGIN
 INSERT INTO change_log(project_id,session_id,event_type,payload) VALUES(NULL,NULL,'session_updated',json_object('instructorInventory',1));
END;
-- +goose StatementEnd
-- +goose Down
DROP TRIGGER instructor_inventory_update;
DROP TRIGGER instructor_inventory_insert;
DROP TABLE instructor_inventory_cache;
