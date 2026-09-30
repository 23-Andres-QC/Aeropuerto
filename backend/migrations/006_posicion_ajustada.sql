-- La Parte III ajusta cada posición al piso transitable: dentro del contorno del piso y
-- fuera de los obstáculos del plano (una carpa, una máquina, un tacho). La posición que dio
-- el modelo queda en raw_x/raw_y (NULL si no hubo que moverla), así el ajuste se vuelve a
-- calcular desde cero cuando cambia el plano.
ALTER TABLE trajectory_points
    ADD COLUMN raw_x DOUBLE PRECISION,
    ADD COLUMN raw_y DOUBLE PRECISION,
    ADD CONSTRAINT trajectory_points_raw_xy CHECK ((raw_x IS NULL) = (raw_y IS NULL));
