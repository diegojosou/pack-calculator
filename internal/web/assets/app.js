const $ = (id) => document.getElementById(id);

const els = {
  rows: $("pack-sizes-rows"),
  addRow: $("add-row"),
  save: $("save-sizes"),
  sizesStatus: $("sizes-status"),
  form: $("calc-form"),
  order: $("order"),
  calcStatus: $("calc-status"),
  result: $("result"),
  resultSummary: $("result-summary"),
  resultRows: $("result-rows"),
};

function addRow(value) {
  const tr = document.createElement("tr");

  const tdInput = document.createElement("td");
  const input = document.createElement("input");
  input.type = "number";
  input.min = "1";
  input.step = "1";
  input.inputMode = "numeric";
  input.placeholder = "e.g. 250";
  if (value != null) input.value = String(value);
  tdInput.appendChild(input);

  const tdActions = document.createElement("td");
  tdActions.style.width = "1%";
  const remove = document.createElement("button");
  remove.type = "button";
  remove.className = "btn-danger";
  remove.setAttribute("aria-label", "Remove pack size");
  remove.textContent = "✕";
  remove.addEventListener("click", () => tr.remove());
  tdActions.appendChild(remove);

  tr.append(tdInput, tdActions);
  els.rows.appendChild(tr);
}

function readRows() {
  const seen = new Set();
  const out = [];
  for (const input of els.rows.querySelectorAll("input")) {
    const raw = input.value.trim();
    if (raw === "") continue;
    const n = Number.parseInt(raw, 10);
    if (Number.isNaN(n) || n <= 0) {
      throw new Error(`"${raw}" is not a valid positive integer`);
    }
    if (!seen.has(n)) {
      seen.add(n);
      out.push(n);
    }
  }
  return out;
}

function renderRows(sizes) {
  els.rows.innerHTML = "";
  for (const size of sizes) addRow(size);
  addRow();
}

function setStatus(el, message, type) {
  el.textContent = message || "";
  el.classList.remove("error", "success");
  if (type) el.classList.add(type);
}

async function api(path, options = {}) {
  const res = await fetch(path, {
    headers: { "Content-Type": "application/json", ...(options.headers || {}) },
    ...options,
  });
  let body = null;
  try { body = await res.json(); } catch (_) { /* tolerate non-JSON */ }
  if (!res.ok) {
    const msg = (body && body.error) || `HTTP ${res.status}`;
    throw new Error(msg);
  }
  return body;
}

async function loadPackSizes() {
  try {
    const body = await api("/api/pack-sizes");
    renderRows(body.pack_sizes || []);
    setStatus(els.sizesStatus, "");
  } catch (err) {
    setStatus(els.sizesStatus, `Failed to load pack sizes: ${err.message}`, "error");
  }
}

async function savePackSizes() {
  let sizes;
  try {
    sizes = readRows();
  } catch (err) {
    setStatus(els.sizesStatus, err.message, "error");
    return;
  }
  try {
    const body = await api("/api/pack-sizes", {
      method: "PUT",
      body: JSON.stringify({ pack_sizes: sizes }),
    });
    renderRows(body.pack_sizes || []);
    setStatus(els.sizesStatus, "Pack sizes saved.", "success");
  } catch (err) {
    setStatus(els.sizesStatus, err.message, "error");
  }
}

async function calculate(event) {
  event.preventDefault();
  const order = Number.parseInt(els.order.value, 10);
  if (Number.isNaN(order) || order <= 0) {
    setStatus(els.calcStatus, "Enter a positive integer for items.", "error");
    return;
  }
  setStatus(els.calcStatus, "Calculating…");
  els.result.hidden = true;

  try {
    const body = await api("/api/calculate", {
      method: "POST",
      body: JSON.stringify({ order }),
    });
    renderResult(body);
    setStatus(els.calcStatus, "");
  } catch (err) {
    setStatus(els.calcStatus, err.message, "error");
  }
}

function renderResult(body) {
  els.resultRows.innerHTML = "";
  for (const line of body.packs) {
    const tr = document.createElement("tr");
    const tdSize = document.createElement("td");
    tdSize.textContent = line.size;
    const tdQty = document.createElement("td");
    tdQty.textContent = line.quantity;
    tr.append(tdSize, tdQty);
    els.resultRows.appendChild(tr);
  }
  els.resultSummary.textContent =
    `Order ${body.order} → ${body.shipped_items} items in ${body.total_packs} pack(s).`;
  els.result.hidden = false;
}

els.addRow.addEventListener("click", () => addRow());
els.save.addEventListener("click", savePackSizes);
els.form.addEventListener("submit", calculate);

loadPackSizes();
