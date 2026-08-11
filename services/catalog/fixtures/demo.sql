-- Deterministic local/demo fixtures. This file is intentionally separate from production migrations.
INSERT INTO catalog_hotels(id,name,description,address,city,country,latitude,longitude,amenities,active) VALUES
('10000000-0000-0000-0000-000000000001','Harbor Hotel','Waterfront business hotel','1 Harbor Way','San Francisco','US',37.7955,-122.3937,ARRAY['wifi','gym'],TRUE),
('10000000-0000-0000-0000-000000000002','Sakura Stay','Central Tokyo hotel','2 Chiyoda','Tokyo','JP',35.6762,139.6503,ARRAY['wifi','breakfast'],TRUE)
ON CONFLICT (id) DO NOTHING;

INSERT INTO catalog_room_types(id,hotel_id,name,description,capacity,bed_count,amenities,active) VALUES
('20000000-0000-0000-0000-000000000001','10000000-0000-0000-0000-000000000001','Queen','One queen bed',2,1,ARRAY['desk'],TRUE),
('20000000-0000-0000-0000-000000000002','10000000-0000-0000-0000-000000000002','Twin','Two twin beds',2,2,ARRAY['desk'],TRUE)
ON CONFLICT (id) DO NOTHING;
