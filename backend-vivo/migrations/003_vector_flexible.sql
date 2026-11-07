-- Con GPU los teléfonos usan el Re-ID de Videos (OSNet-AIN y PersonViT juntos, 896 valores); sin GPU sigue yolo26s-reid
-- (512). Las columnas pasan a vector sin dimensión fija y el backend acepta solo esas dos (memoria.Dimensiones); una
-- persona usa una sola. Los vectores guardados no cambian.
ALTER TABLE personas ALTER COLUMN suma TYPE vector;
ALTER TABLE vistas ALTER COLUMN prototipo TYPE vector;
