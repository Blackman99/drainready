(() => {
  const NS = "http://www.w3.org/2000/svg";
  const svg = document.getElementById("scene");
  const statusEl = document.getElementById("status");
  const runBtn = document.getElementById("run");
  const fix = document.getElementById("fix");
  const fixLabel = document.getElementById("fixlabel");
  const reduce = matchMedia("(prefers-reduced-motion: reduce)").matches;
  const C = { navy: "#13283f", rust: "#b8432f", rustd: "#8f3222", steel: "#3d6a9e", bone: "#f1ead9", sea: "#6f8ea3", conc: "#c9c3b4", haze: "#e6ecef" };
  const W = 132, H = 48, BOOM = 96;
  const el = (n, a, p = svg) => { const e = document.createElementNS(NS, n); for (const k in a) e.setAttribute(k, a[k]); p.appendChild(e); return e; };

  // static scenery
  const defs = el("defs", {});
  for (const [id, col] of [["rib-r", "#00000022"], ["rib-l", "#ffffff22"]]) {
    const p = el("pattern", { id, width: 10, height: 10, patternUnits: "userSpaceOnUse" }, defs);
    el("rect", { x: 0, y: 0, width: 3, height: 10, fill: col }, p);
  }
  el("rect", { x: 0, y: 470, width: 1200, height: 90, fill: C.sea });
  for (let i = 0; i < 9; i++) el("path", { d: `M${i * 140 - 20} ${500 + (i % 3) * 16}h70`, stroke: C.haze, "stroke-width": 3, opacity: .5, class: "wave" });
  el("rect", { x: 0, y: 430, width: 342, height: 130, fill: C.conc });
  el("rect", { x: 0, y: 430, width: 342, height: 8, fill: C.navy });
  // quay bollards + painted bay marks (flat, no glow)
  for (const x of [48, 148, 248]) {
    el("circle", { cx: x, cy: 448, r: 10, fill: C.navy });
    el("rect", { x: x - 4, y: 438, width: 8, height: 14, fill: C.rust });
  }
  const quayMark = el("text", { x: 14, y: 520, "font-family": "BSS", "font-weight": 800, "font-size": 14, fill: C.navy, opacity: .55, "letter-spacing": 3 });
  quayMark.textContent = "QUAY · BAY A";
  el("path", { d: "M350 340H1190L1150 470H380Z", fill: C.navy });
  el("rect", { x: 350, y: 334, width: 840, height: 6, fill: C.rust });
  // crane
  el("path", { d: "M298 430V80M340 430V80M298 300L340 200M340 300L298 200", stroke: C.navy, "stroke-width": 8, fill: "none" });
  el("rect", { x: 140, y: 80, width: 1055, height: 16, fill: C.navy });
  el("path", { d: "M298 80L319 30L340 80M319 30L1190 80M319 30L150 80", stroke: C.navy, "stroke-width": 3, fill: "none" });
  const trolley = el("g", {});
  el("rect", { x: -22, y: 96, width: 44, height: 12, fill: C.rust }, trolley);
  const cable = el("path", { stroke: C.navy, "stroke-width": 2 });
  const spreader = el("g", {});
  el("rect", { x: -W / 2 - 4, y: -8, width: W + 8, height: 8, fill: C.rust }, spreader);
  const boxes = el("g", {});
  const stencil = el("text", { x: 770, y: 228, "text-anchor": "middle", "font-family": "BSS", "font-weight": 800, "font-size": 64, fill: C.rust, opacity: 0, "letter-spacing": 4 });

  const crane = { x: 760, y: 150 };
  function drawCrane() {
    trolley.setAttribute("transform", `translate(${crane.x} 0)`);
    spreader.setAttribute("transform", `translate(${crane.x} ${crane.y})`);
    cable.setAttribute("d", `M${crane.x - 18} 108V${crane.y - 8}M${crane.x + 18} 108V${crane.y - 8}`);
  }

  const PODS = [
    ["web-7d9-a1", C.steel], ["api-5f4-q8", C.bone], ["web-7d9-x2k", C.sea, "wait"], ["worker-0", C.steel],
    ["api-5f4-m3", C.rust], ["cache-0", C.bone], ["search-1", C.steel], ["db-0", C.rust, "pdb"],
  ];
  let boxesState = [];
  function build() {
    boxes.innerHTML = "";
    boxesState = PODS.map(([name, col, kind], i) => {
      const tier = i < 4 ? 1 : 0, colI = i % 4;
      const g = el("g", {}, boxes);
      el("rect", { x: -W / 2, y: -H, width: W, height: H, fill: col }, g);
      el("rect", { x: -W / 2, y: -H, width: W, height: H, fill: col === C.bone ? "url(#rib-r)" : "url(#rib-l)" }, g);
      el("rect", { x: -W / 2, y: -H, width: W, height: 5, fill: "#0003" }, g);
      const t = el("text", { x: 0, y: -18, "text-anchor": "middle", "font-family": "PlexM", "font-size": 12, fill: col === C.bone || col === C.sea ? C.navy : C.bone }, g);
      t.textContent = name;
      let lash = null;
      if (kind === "pdb") {
        lash = el("g", {}, g);
        el("path", { d: `M${-W / 2} ${-H}L${-W / 2 + 26} 4M${W / 2} ${-H}L${W / 2 - 26} 4`, stroke: C.navy, "stroke-width": 4 }, lash);
        const tag = el("text", { x: 0, y: -33, "text-anchor": "middle", "font-family": "BSS", "font-weight": 800, "font-size": 13, fill: C.bone, "letter-spacing": 2 }, lash);
        tag.textContent = "LASHED · PDB 0";
      }
      const b = { g, name, kind, lash, x: 440 + colI * 140 + W / 2, y: 340 - tier * H, home: [440 + colI * 140 + W / 2, 340 - tier * H] };
      place(b);
      return b;
    });
    stencil.setAttribute("opacity", 0);
    crane.x = 760; crane.y = 150; drawCrane();
  }
  const place = b => b.g.setAttribute("transform", `translate(${b.x} ${b.y})`);

  const tween = (obj, to, ms, onStep) => new Promise(res => {
    if (reduce) { Object.assign(obj, to); onStep(); return res(); }
    const from = {}; for (const k in to) from[k] = obj[k];
    const t0 = performance.now();
    const ease = t => t < .5 ? 2 * t * t : 1 - (-2 * t + 2) ** 2 / 2;
    (function f(now) {
      const t = Math.min(1, (now - t0) / ms), e = ease(t);
      for (const k in to) obj[k] = from[k] + (to[k] - from[k]) * e;
      onStep();
      t < 1 ? requestAnimationFrame(f) : res();
    })(t0);
  });
  const sleep = ms => reduce ? Promise.resolve() : new Promise(r => setTimeout(r, ms));
  const say = s => { statusEl.textContent = s; };

  let running = false, token = 0;
  async function unload() {
    if (running) { token++; }
    const my = ++token; running = true; runBtn.disabled = true;
    build();
    const fixed = fix.checked;
    const slots = []; for (let c = 0; c < 2; c++) for (let t = 0; t < 4; t++) slots.push([14 + c * 142 + W / 2, 430 - t * H]);
    let s = 0, ok = 0;
    for (const b of boxesState) {
      if (my !== token) return;
      await tween(crane, { x: b.x }, 420, drawCrane);
      await tween(crane, { y: b.y - H }, 300, drawCrane);
      if (b.kind === "pdb" && !fixed) {
        say(`evicting shop/${b.name} … 429 Too Many Requests`);
        for (let k = 0; k < 3; k++) {
          await tween(crane, { y: b.y - H - 7 }, 110, drawCrane);
          await tween(crane, { y: b.y - H }, 110, drawCrane);
        }
        await tween(crane, { y: 150 }, 300, drawCrane);
        stencil.textContent = "BLOCKED"; stencil.setAttribute("opacity", 1);
        say(`BLOCKED  shop/db-0: PDB shop/db allows 0 disruptions (healthy 2, needs 2). kubectl drain would retry forever.`);
        break;
      }
      if (b.kind === "wait") { say(`WAIT  shop/${b.name}: waiting for its replacement to turn ready elsewhere…`); await sleep(600); }
      else say(`evicting shop/${b.name} … ok`);
      if (b.lash) b.lash.remove();
      const follow = () => { b.x = crane.x; b.y = crane.y + H; place(b); drawCrane(); };
      await tween(crane, { y: BOOM + 40 }, 340, follow);
      const [sx, sy] = slots[s++];
      await tween(crane, { x: sx }, 520, follow);
      await tween(crane, { y: sy - H }, 320, follow);
      ok++;
      await tween(crane, { y: 150 }, 220, drawCrane);
    }
    if (my !== token) return;
    if (fixed) { stencil.textContent = "READY"; stencil.setAttribute("fill", C.steel); stencil.setAttribute("opacity", 1); say(`READY  ${ok} pods evicted. Safe to cordon and drain.`); }
    else stencil.setAttribute("fill", C.rust);
    running = false; runBtn.disabled = false; runBtn.textContent = "Unload again";
  }

  fix.addEventListener("change", () => {
    fixLabel.textContent = fix.checked ? "maxUnavailable: 1" : "minAvailable: 2";
    unload();
  });
  runBtn.addEventListener("click", unload);
  build();
  const io = new IntersectionObserver(es => { if (es[0].isIntersecting) { io.disconnect(); unload(); } });
  io.observe(svg);

  // stack verdict containers as they scroll in
  const items = document.querySelectorAll(".stack li");
  if (!reduce && "IntersectionObserver" in window) {
    items.forEach((li, i) => { li.classList.add("reveal"); li.style.transitionDelay = (items.length - i) * 90 + "ms"; });
    const io2 = new IntersectionObserver(es => es.forEach(e => { if (e.isIntersecting) { e.target.classList.add("in"); io2.unobserve(e.target); } }), { threshold: .4 });
    items.forEach(li => io2.observe(li));
  }
})();
