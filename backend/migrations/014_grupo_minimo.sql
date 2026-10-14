-- Tamaño mínimo de grupo por sitio: una categoría de género con menos personas no se muestra (ver domain.PrivateGender).
-- 5 por defecto; solo un admin_lap lo baja (hasta 1) para verificar la detección en sitios de prueba. La tienda usa siempre 5.
ALTER TABLE sites ADD COLUMN min_group_size INTEGER NOT NULL DEFAULT 5 CHECK (min_group_size BETWEEN 1 AND 50);
