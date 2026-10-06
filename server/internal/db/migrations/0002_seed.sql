-- Seeded plans: limits presets with no prices. -1 means unlimited.
insert into plans (id, kind, name, limits, is_default, sort) values
  ('postgres-small',  'postgres', 'Small',  '{"storage_mb":1024,"direct_connections":5,"pooled_connections":20,"statement_timeout_ms":30000,"work_mem_mb":4,"requests_per_month":100000,"backup_days":3}', true, 1),
  ('postgres-medium', 'postgres', 'Medium', '{"storage_mb":10240,"direct_connections":15,"pooled_connections":60,"statement_timeout_ms":60000,"work_mem_mb":16,"requests_per_month":1000000,"backup_days":7}', false, 2),
  ('postgres-large',  'postgres', 'Large',  '{"storage_mb":51200,"direct_connections":40,"pooled_connections":200,"statement_timeout_ms":300000,"work_mem_mb":64,"requests_per_month":-1,"backup_days":14}', false, 3),

  ('cache-small',  'cache', 'Small',  '{"memory_mb":32,"connections":10,"requests_per_month":100000}', true, 1),
  ('cache-medium', 'cache', 'Medium', '{"memory_mb":256,"connections":50,"requests_per_month":1000000}', false, 2),
  ('cache-large',  'cache', 'Large',  '{"memory_mb":1024,"connections":200,"requests_per_month":-1}', false, 3),

  ('storage-small',  'storage', 'Small',  '{"storage_mb":2048,"max_upload_mb":50,"requests_per_month":100000}', true, 1),
  ('storage-medium', 'storage', 'Medium', '{"storage_mb":25600,"max_upload_mb":200,"requests_per_month":1000000}', false, 2),
  ('storage-large',  'storage', 'Large',  '{"storage_mb":102400,"max_upload_mb":1024,"requests_per_month":-1}', false, 3),

  ('vector-small',  'vector', 'Small',  '{"storage_mb":1024,"max_dimension":2048,"direct_connections":5,"statement_timeout_ms":30000,"work_mem_mb":16,"requests_per_month":100000,"backup_days":3}', true, 1),
  ('vector-medium', 'vector', 'Medium', '{"storage_mb":10240,"max_dimension":4096,"direct_connections":15,"statement_timeout_ms":60000,"work_mem_mb":64,"requests_per_month":1000000,"backup_days":7}', false, 2),
  ('vector-large',  'vector', 'Large',  '{"storage_mb":51200,"max_dimension":8192,"direct_connections":40,"statement_timeout_ms":300000,"work_mem_mb":128,"requests_per_month":-1,"backup_days":14}', false, 3),

  ('queue-small',  'queue', 'Small',  '{"storage_mb":512,"max_message_kb":256,"direct_connections":5,"statement_timeout_ms":30000,"work_mem_mb":4,"requests_per_month":100000,"backup_days":3}', true, 1),
  ('queue-medium', 'queue', 'Medium', '{"storage_mb":5120,"max_message_kb":1024,"direct_connections":15,"statement_timeout_ms":60000,"work_mem_mb":8,"requests_per_month":1000000,"backup_days":7}', false, 2),
  ('queue-large',  'queue', 'Large',  '{"storage_mb":20480,"max_message_kb":4096,"direct_connections":40,"statement_timeout_ms":300000,"work_mem_mb":16,"requests_per_month":-1,"backup_days":14}', false, 3),

  ('auth-small',  'auth', 'Small',  '{"max_users":10000,"storage_mb":512,"direct_connections":5,"statement_timeout_ms":30000,"work_mem_mb":4,"requests_per_month":100000,"backup_days":3}', true, 1),
  ('auth-medium', 'auth', 'Medium', '{"max_users":100000,"storage_mb":5120,"direct_connections":15,"statement_timeout_ms":60000,"work_mem_mb":8,"requests_per_month":1000000,"backup_days":7}', false, 2),
  ('auth-large',  'auth', 'Large',  '{"max_users":-1,"storage_mb":20480,"direct_connections":40,"statement_timeout_ms":300000,"work_mem_mb":16,"requests_per_month":-1,"backup_days":14}', false, 3)
on conflict (id) do nothing;

insert into settings (key, value) values
  ('signup_mode', '"invite"'),
  ('auto_pause_days', '0'),
  ('resource_cap', '10'),
  ('members_change_plans', 'true')
on conflict (key) do nothing;
