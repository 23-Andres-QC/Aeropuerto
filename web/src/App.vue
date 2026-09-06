<template>
  <header class="header">
    <a class="brand" href="/mapa"
      ><span class="brand-icon">↗</span
      ><span>AEROPUERTO<small>OBSERVATORIO DE FLUJO</small></span></a
    >
    <nav aria-label="Navegación principal">
      <RouterLink to="/mapa">Mapa en vivo</RouterLink
      ><RouterLink to="/insights">Insights</RouterLink>
    </nav>
    <span class="demo">DATOS SIMULADOS</span>
  </header>
  <main><RouterView /></main>
  <footer>
    Jorge Chávez · Prototipo de análisis de movimiento<span
      >Mapa real · Sin cámaras conectadas</span
    >
  </footer>
</template>
