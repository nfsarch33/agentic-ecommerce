-- Fitness and wellness product seed data for development.
-- Prices are in cents (AUD). Run after migrations/0001_create_products.up.sql.

INSERT INTO categories (id, name, slug) VALUES
    ('a1000000-0000-0000-0000-000000000001', 'Strength Training', 'strength-training'),
    ('a1000000-0000-0000-0000-000000000002', 'Recovery', 'recovery'),
    ('a1000000-0000-0000-0000-000000000003', 'Nutrition', 'nutrition'),
    ('a1000000-0000-0000-0000-000000000004', 'Cardio', 'cardio'),
    ('a1000000-0000-0000-0000-000000000005', 'Yoga & Flexibility', 'yoga-flexibility')
ON CONFLICT (id) DO NOTHING;

INSERT INTO products (id, sku, title, slug, description, price_amount, price_currency, stock, status) VALUES
    ('b1000000-0000-0000-0000-000000000001', 'RB-SET-5', 'Resistance Band Set (5 Levels)', 'resistance-band-set-5-levels',
     'Progressive resistance band set with 5 tension levels from 5lb to 40lb. Includes door anchor, ankle straps, and carry bag.',
     4995, 'AUD', 120, 'active'),
    ('b1000000-0000-0000-0000-000000000002', 'FR-HD-45', 'High-Density Foam Roller 45cm', 'high-density-foam-roller-45cm',
     'Dense EVA foam roller for deep tissue massage and post-workout recovery. Textured surface for targeted pressure.',
     3500, 'AUD', 85, 'active'),
    ('b1000000-0000-0000-0000-000000000003', 'KB-CAST-16', 'Cast Iron Kettlebell 16kg', 'cast-iron-kettlebell-16kg',
     'Powder-coated cast iron kettlebell with wide handle for two-hand swings. Flat base for floor stability.',
     8900, 'AUD', 30, 'active'),
    ('b1000000-0000-0000-0000-000000000004', 'YM-TPE-6', 'Non-Slip Yoga Mat 6mm', 'non-slip-yoga-mat-6mm',
     'Eco-friendly TPE yoga mat with dual-layer non-slip texture. 183cm x 61cm, includes carry strap.',
     5495, 'AUD', 200, 'active'),
    ('b1000000-0000-0000-0000-000000000005', 'SR-SPEED', 'Speed Jump Rope (Adjustable)', 'speed-jump-rope-adjustable',
     'Ball-bearing steel cable jump rope with adjustable length up to 3m. Ergonomic aluminium handles.',
     2495, 'AUD', 150, 'active'),
    ('b1000000-0000-0000-0000-000000000006', 'WP-ISO-1K', 'Whey Protein Isolate 1kg (Vanilla)', 'whey-protein-isolate-1kg-vanilla',
     'Cold-filtered whey protein isolate with 30g protein per serve. Grass-fed, no artificial sweeteners.',
     6995, 'AUD', 60, 'active'),
    ('b1000000-0000-0000-0000-000000000007', 'MB-SLAM-9', 'Slam Ball 9kg', 'slam-ball-9kg',
     'Dead-bounce slam ball with textured rubber shell. Sand-filled for no-roll stability during HIIT workouts.',
     5900, 'AUD', 45, 'active'),
    ('b1000000-0000-0000-0000-000000000008', 'PU-BAR-DIP', 'Doorway Pull-Up & Dip Bar', 'doorway-pull-up-dip-bar',
     'Multi-grip pull-up bar with padded dip station attachment. Fits door frames 65-90cm wide, no screws required.',
     7495, 'AUD', 35, 'draft'),
    ('b1000000-0000-0000-0000-000000000009', 'MG-GUN-PRO', 'Percussion Massage Gun Pro', 'percussion-massage-gun-pro',
     'Brushless motor massage gun with 6 speed levels and 4 interchangeable heads. USB-C rechargeable, 6-hour battery.',
     12900, 'AUD', 25, 'active'),
    ('b1000000-0000-0000-0000-000000000010', 'CR-MONO-500', 'Creatine Monohydrate 500g', 'creatine-monohydrate-500g',
     'Micronised creatine monohydrate powder. 100 serves per container, unflavoured, mixes clear.',
     3495, 'AUD', 90, 'active')
ON CONFLICT (id) DO NOTHING;

INSERT INTO product_categories (product_id, category_id) VALUES
    ('b1000000-0000-0000-0000-000000000001', 'a1000000-0000-0000-0000-000000000001'),
    ('b1000000-0000-0000-0000-000000000002', 'a1000000-0000-0000-0000-000000000002'),
    ('b1000000-0000-0000-0000-000000000003', 'a1000000-0000-0000-0000-000000000001'),
    ('b1000000-0000-0000-0000-000000000004', 'a1000000-0000-0000-0000-000000000005'),
    ('b1000000-0000-0000-0000-000000000005', 'a1000000-0000-0000-0000-000000000004'),
    ('b1000000-0000-0000-0000-000000000006', 'a1000000-0000-0000-0000-000000000003'),
    ('b1000000-0000-0000-0000-000000000007', 'a1000000-0000-0000-0000-000000000001'),
    ('b1000000-0000-0000-0000-000000000007', 'a1000000-0000-0000-0000-000000000004'),
    ('b1000000-0000-0000-0000-000000000008', 'a1000000-0000-0000-0000-000000000001'),
    ('b1000000-0000-0000-0000-000000000009', 'a1000000-0000-0000-0000-000000000002'),
    ('b1000000-0000-0000-0000-000000000010', 'a1000000-0000-0000-0000-000000000003')
ON CONFLICT DO NOTHING;

INSERT INTO product_images (id, product_id, url, alt, sort_order) VALUES
    ('c1000000-0000-0000-0000-000000000001', 'b1000000-0000-0000-0000-000000000001',
     '/images/products/resistance-band-set.jpg', 'Resistance band set with 5 colour-coded bands', 0),
    ('c1000000-0000-0000-0000-000000000002', 'b1000000-0000-0000-0000-000000000002',
     '/images/products/foam-roller.jpg', 'Black high-density foam roller', 0),
    ('c1000000-0000-0000-0000-000000000003', 'b1000000-0000-0000-0000-000000000003',
     '/images/products/kettlebell-16kg.jpg', 'Black cast iron kettlebell 16kg', 0),
    ('c1000000-0000-0000-0000-000000000004', 'b1000000-0000-0000-0000-000000000004',
     '/images/products/yoga-mat.jpg', 'Teal non-slip yoga mat rolled', 0),
    ('c1000000-0000-0000-0000-000000000005', 'b1000000-0000-0000-0000-000000000005',
     '/images/products/jump-rope.jpg', 'Speed jump rope with aluminium handles', 0)
ON CONFLICT (id) DO NOTHING;
