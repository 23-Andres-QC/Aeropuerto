// Build a local, vertically oriented SVG from a GeoJSON feature array.
// The building runs north-south: north points up, east points right.
// Usage: node scripts/maps/build-plan.mjs /tmp/airport-features.json
import fs from 'node:fs';
const data=JSON.parse(fs.readFileSync(process.argv[2],'utf8'));
// -25.8 aligns the terminal with the axes; +90 turns the long side vertical, north up.
const origin=[-77.116766,-12.028493],angle=(-25.8+90)*Math.PI/180;
function project(p){const east=(p[0]-origin[0])*111320*Math.cos(origin[1]*Math.PI/180),north=(p[1]-origin[1])*111320;return [east*Math.sin(angle)+north*Math.cos(angle),east*Math.cos(angle)-north*Math.sin(angle)]}
const all=[];function visit(g){if(typeof g[0]==='number')all.push(project(g));else g.forEach(visit)};data.forEach(f=>visit(f.geometry.coordinates));
const minX=Math.min(...all.map(p=>p[0]))-18,minY=Math.min(...all.map(p=>p[1]))-18,maxX=Math.max(...all.map(p=>p[0]))+18,maxY=Math.max(...all.map(p=>p[1]))+18;
const width=maxX-minX,height=maxY-minY;
function line(r,close=false){return r.map((p,i)=>{const [x,y]=project(p);return `${i?'L':'M'}${(x-minX).toFixed(2)},${(y-minY).toFixed(2)}`}).join(' ')+(close?'Z':'')}
function path(g){if(g.type==='Polygon')return g.coordinates.map(r=>line(r,true)).join(' ');if(g.type==='MultiPolygon')return g.coordinates.flatMap(p=>p.map(r=>line(r,true))).join(' ');if(g.type==='LineString')return line(g.coordinates);if(g.type==='MultiLineString')return g.coordinates.map(r=>line(r)).join(' ');return ''}
function fill(p){if(p.type==='floor'||p.type==='corridor')return '#ffffff';if(p.class==='retail')return '#f6e9b6';if(p.class==='food_and_drink')return '#ead9bc';if(p.class==='restroom')return '#d5e9ec';if(p.class==='circulation')return '#d4e6e8';if(p.type==='waiting_area')return '#e7edf7';if(p.type==='back_of_house')return '#e6ebef';return '#edf1f4'}
const rank=f=>f.properties.type==='floor'?0:f.properties.type==='corridor'?1:f.geometry.type.includes('Polygon')?2:3;
const shapes=data.filter(f=>f.geometry.type!=='Point').sort((a,b)=>rank(a)-rank(b)).map(f=>`<path d="${path(f.geometry)}" fill="${f.geometry.type.includes('Polygon')?fill(f.properties):'none'}" fill-rule="evenodd" stroke="${f.properties.type==='wall'?'#597386':'#9eafb8'}" stroke-width="${f.properties.type==='wall'?.65:.3}"/>`);
fs.writeFileSync('web/public/maps/airport-level-3.svg',`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 ${width} ${height}"><title>Aeropuerto Jorge Chávez · Nivel 3</title><desc>Plano local derivado de geometrías públicas de Lima Airport / Living Map, 2026-09-06. Orientación vertical, norte hacia arriba. No constituye calibración de cámaras.</desc><rect width="100%" height="100%" fill="#f4f9ff"/>${shapes.join('\n')}</svg>`);
fs.writeFileSync('web/src/plan.json',JSON.stringify({origin,angle,minX,minY,width,height,source:'Lima Airport / Living Map',floor:3,date:'2026-09-06'},null,2));
console.log({width,height,features:data.length});
