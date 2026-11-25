(() => {
  const TOKEN_KEY = "cohavora_org_admin_token";
  const state = {
    token: sessionStorage.getItem(TOKEN_KEY) || "",
    founder: null,
    orgCode: "",
    serverVersion: 0,
    base: { departments: [], members: [], users: [], employeeRecords: [], memberVisibility: "all" },
    draft: { departments: [], members: [], userUpdates: {}, removalReasons: {}, memberVisibility: "all" },
    collapsed: new Set(),
    viewMode: "users", // "dept" | "users" | "history"
    selectedDeptID: "",
    userFilter: "all",
    historyFilter: "all",
    deptSearch: "",
    memberSearch: "",
    allUsersSearch: "",
    historySearch: "",
    tmpSeq: 1,
  };

  const $ = (id) => document.getElementById(id);

  function clone(v) {
    return JSON.parse(JSON.stringify(v));
  }

  function escapeHtml(str) {
    return String(str ?? "")
      .replace(/&/g, "&amp;")
      .replace(/</g, "&lt;")
      .replace(/>/g, "&gt;")
      .replace(/"/g, "&quot;");
  }

  function formatTime(ms) {
    if (!ms) return "—";
    const d = new Date(ms);
    if (Number.isNaN(d.getTime())) return "—";
    const pad = (n) => String(n).padStart(2, "0");
    return `${d.getFullYear()}-${pad(d.getMonth() + 1)}-${pad(d.getDate())} ${pad(d.getHours())}:${pad(d.getMinutes())}`;
  }

  function toast(msg, type = "info") {
    const el = document.createElement("div");
    el.className = `toast toast-${type}`;
    el.textContent = msg;
    $("toastContainer").appendChild(el);
    setTimeout(() => el.remove(), 4200);
  }

  function genOpId() {
    return "web-" + Date.now().toString(36) + "-" + Math.random().toString(36).slice(2, 8);
  }

  async function api(method, path, body = null, isFormData = false) {
    const headers = { operationID: genOpId() };
    if (state.token) headers.token = state.token;
    if (!isFormData && body !== null) headers["Content-Type"] = "application/json";
    const res = await fetch(path, {
      method,
      headers,
      body: body === null ? undefined : isFormData ? body : JSON.stringify(body),
    });
    let payload = null;
    try {
      payload = await res.json();
    } catch {
      payload = { errCode: res.status, errMsg: res.statusText || "请求失败" };
    }
    if (res.status === 401) {
      logoutLocal("登录已过期，请重新登录。");
      throw new Error(payload.errMsg || "登录已过期");
    }
    if (!res.ok || (payload && payload.errCode && payload.errCode !== 0)) {
      const msg = (payload && (payload.errDlt || payload.errMsg)) || `请求失败 (${res.status})`;
      throw new Error(msg);
    }
    return payload ? payload.data : null;
  }

  function showLogin(errorMsg = "") {
    $("appView").classList.add("hidden");
    $("loginView").classList.remove("hidden");
    const errEl = $("loginError");
    if (errorMsg) {
      errEl.textContent = errorMsg;
      errEl.classList.remove("hidden");
    } else {
      errEl.classList.add("hidden");
    }
  }

  function showApp() {
    $("loginView").classList.add("hidden");
    $("appView").classList.remove("hidden");
  }

  function logoutLocal(msg = "") {
    state.token = "";
    state.founder = null;
    sessionStorage.removeItem(TOKEN_KEY);
    closeModal();
    showLogin(msg);
  }

  // ---- Draft & Diff helpers ----

  function resetDraftFromBase() {
    state.draft.departments = clone(state.base.departments);
    state.draft.members = clone(state.base.members);
    state.draft.userUpdates = {};
    state.draft.removalReasons = {};
    state.draft.memberVisibility = state.base.memberVisibility || "all";
  }

  function formatVisLabel(v) {
    return v === "own_dept" ? "仅可查看本部门成员" : "可查看全部成员";
  }

  function formatDeptVisShort(v) {
    if (v === "own_dept") return "仅本部门";
    if (v === "all") return "可查全部";
    return "继承默认";
  }

  function effectiveUser(userID) {
    const orig = state.base.users.find((u) => u.userID === userID);
    if (!orig) return null;
    const upd = state.draft.userUpdates[userID];
    return upd ? { ...orig, ...upd } : orig;
  }

  function wasInBaseOrg(userID) {
    return state.base.members.some((m) => m.userID === userID);
  }

  function isInDraftOrg(userID) {
    return state.draft.members.some((m) => m.userID === userID);
  }

  function userEmployeeRecords(userID) {
    return (state.base.employeeRecords || []).filter((r) => r.userID === userID);
  }

  function deptById(deptID, list = state.draft.departments) {
    return list.find((d) => d.deptID === deptID) || null;
  }

  function getDescendantIds(deptID) {
    const out = new Set();
    const queue = [deptID];
    while (queue.length) {
      const cur = queue.shift();
      for (const d of state.draft.departments) {
        if (d.parentID === cur && !out.has(d.deptID)) {
          out.add(d.deptID);
          queue.push(d.deptID);
        }
      }
    }
    return out;
  }

  function getDeptBreadcrumb(deptID) {
    const names = [];
    const seen = new Set();
    let cur = deptById(deptID);
    while (cur && !seen.has(cur.deptID)) {
      seen.add(cur.deptID);
      names.unshift(cur.name);
      cur = cur.parentID ? deptById(cur.parentID) : null;
    }
    return names.join(" / ");
  }

  function computeDiff() {
    const baseDeptMap = new Map(state.base.departments.map((d) => [d.deptID, d]));
    const draftDeptMap = new Map(state.draft.departments.map((d) => [d.deptID, d]));

    const addedDepts = [];
    const modifiedDepts = [];
    const deletedDepts = [];

    for (const d of state.draft.departments) {
      const prev = baseDeptMap.get(d.deptID);
      if (!prev) {
        addedDepts.push(d);
      } else if (
        prev.name !== d.name ||
        (prev.parentID || "") !== (d.parentID || "") ||
        Number(prev.sortOrder || 0) !== Number(d.sortOrder || 0) ||
        (prev.memberVisibility || "") !== (d.memberVisibility || "")
      ) {
        modifiedDepts.push({ prev, next: d });
      }
    }
    for (const d of state.base.departments) {
      if (!draftDeptMap.has(d.deptID)) deletedDepts.push(d);
    }

    const memKey = (m) => `${m.deptID}\0${m.userID}`;
    const baseMemMap = new Map(state.base.members.map((m) => [memKey(m), m]));
    const draftMemMap = new Map(state.draft.members.map((m) => [memKey(m), m]));

    let addedMembers = 0;
    let removedMembers = 0;
    let roleChangedMembers = 0;
    for (const [k, m] of draftMemMap) {
      const prev = baseMemMap.get(k);
      if (!prev) addedMembers++;
      else if (Boolean(prev.isLeader) !== Boolean(m.isLeader)) roleChangedMembers++;
    }
    for (const k of baseMemMap.keys()) {
      if (!draftMemMap.has(k)) removedMembers++;
    }

    const baseOrgUsers = new Set(state.base.members.map((m) => m.userID));
    const draftOrgUsers = new Set(state.draft.members.map((m) => m.userID));
    const newlyJoinedOrg = [];
    const newlyLeftOrg = [];
    for (const uid of draftOrgUsers) {
      if (!baseOrgUsers.has(uid)) newlyJoinedOrg.push(uid);
    }
    for (const uid of baseOrgUsers) {
      if (!draftOrgUsers.has(uid)) newlyLeftOrg.push(uid);
    }

    const editedUsers = Object.keys(state.draft.userUpdates);
    const visibilityChanged = (state.base.memberVisibility || "all") !== (state.draft.memberVisibility || "all");
    const totalChanges =
      addedDepts.length +
      modifiedDepts.length +
      deletedDepts.length +
      addedMembers +
      removedMembers +
      roleChangedMembers +
      editedUsers.length +
      (visibilityChanged ? 1 : 0);

    return {
      totalChanges,
      visibilityChanged,
      addedDepts,
      modifiedDepts,
      deletedDepts,
      addedMembers,
      removedMembers,
      roleChangedMembers,
      newlyJoinedOrg,
      newlyLeftOrg,
      editedUsers,
    };
  }

  // ---- Rendering ----

  function renderAll() {
    renderHeaderAndDirtyBar();
    renderDeptTree();
    renderMainPanel();
  }

  function renderHeaderAndDirtyBar() {
    $("versionBadge").textContent = state.serverVersion ? `版本: v${state.serverVersion}` : "版本: 未初始化";
    if (state.founder) {
      const label = state.founder.nickname ? `${state.founder.nickname} (${state.founder.account})` : state.founder.account;
      $("founderLabel").textContent = `创始人 · ${label}`;
    }
    $("allUsersCountBadge").textContent = String(state.base.users.length);
    $("historyCountBadge").textContent = String((state.base.employeeRecords || []).length);
    $("deptCountBadge").textContent = String(state.draft.departments.length);
    $("btnViewAllUsers").classList.toggle("active", state.viewMode === "users");
    $("btnViewHistory").classList.toggle("active", state.viewMode === "history");

    const curVis = state.draft.memberVisibility || "all";
    for (const b of $("orgVisibilityGroup").querySelectorAll(".seg-btn")) {
      b.classList.toggle("active", b.dataset.vis === curVis);
    }

    const diff = computeDiff();
    if (diff.totalChanges > 0) {
      const parts = [];
      if (diff.visibilityChanged) parts.push(`默认权限改为「${formatVisLabel(curVis)}」`);
      const deptDelta = diff.addedDepts.length + diff.modifiedDepts.length + diff.deletedDepts.length;
      const memDelta = diff.addedMembers + diff.removedMembers + diff.roleChangedMembers;
      if (deptDelta > 0) parts.push(`${deptDelta} 项部门/权限变更`);
      if (memDelta > 0) parts.push(`${memDelta} 项成员归属/主管变更`);
      if (diff.editedUsers.length > 0) parts.push(`${diff.editedUsers.length} 位成员职位修改`);
      $("dirtySummaryText").textContent = `待应用修改：${parts.join("，")}`;
      $("dirtyBar").classList.remove("hidden");
    } else {
      $("dirtyBar").classList.add("hidden");
    }
  }

  function renderDeptTree() {
    const container = $("deptTreeContainer");
    const emptyEl = $("deptTreeEmpty");
    container.innerHTML = "";

    const depts = state.draft.departments;
    if (!depts.length) {
      emptyEl.classList.remove("hidden");
      return;
    }
    emptyEl.classList.add("hidden");

    const memberCounts = new Map();
    for (const m of state.draft.members) {
      memberCounts.set(m.deptID, (memberCounts.get(m.deptID) || 0) + 1);
    }

    const kw = state.deptSearch.trim().toLowerCase();
    const matchSet = new Set();
    if (kw) {
      for (const d of depts) {
        if (d.name.toLowerCase().includes(kw)) {
          matchSet.add(d.deptID);
          let p = d.parentID;
          while (p) {
            matchSet.add(p);
            const pd = deptById(p);
            p = pd ? pd.parentID : "";
          }
        }
      }
    }

    const byParent = new Map();
    const idSet = new Set(depts.map((d) => d.deptID));
    for (const d of depts) {
      const p = d.parentID && idSet.has(d.parentID) ? d.parentID : "";
      if (!byParent.has(p)) byParent.set(p, []);
      byParent.get(p).push(d);
    }
    for (const list of byParent.values()) {
      list.sort((a, b) => (a.sortOrder || 0) - (b.sortOrder || 0) || a.name.localeCompare(b.name, "zh-CN"));
    }

    function renderBranch(parentID, depth) {
      const list = byParent.get(parentID) || [];
      for (const d of list) {
        if (kw && !matchSet.has(d.deptID)) continue;
        const children = byParent.get(d.deptID) || [];
        const isCollapsed = !kw && state.collapsed.has(d.deptID);
        const row = document.createElement("div");
        row.className = "tree-row" + (state.viewMode === "dept" && state.selectedDeptID === d.deptID ? " active" : "");
        row.style.paddingLeft = `${8 + depth * 18}px`;

        const toggleBtn = document.createElement("button");
        toggleBtn.className = "tree-toggle";
        toggleBtn.textContent = children.length ? (isCollapsed ? "▸" : "▾") : "•";
        if (children.length) {
          toggleBtn.addEventListener("click", (e) => {
            e.stopPropagation();
            if (state.collapsed.has(d.deptID)) state.collapsed.delete(d.deptID);
            else state.collapsed.add(d.deptID);
            renderDeptTree();
          });
        }
        row.appendChild(toggleBtn);

        const nameSpan = document.createElement("span");
        nameSpan.className = "tree-name";
        nameSpan.textContent = d.name;
        nameSpan.title = d.name;
        row.appendChild(nameSpan);

        if (d.memberVisibility === "own_dept" || d.memberVisibility === "all") {
          const visTag = document.createElement("span");
          visTag.className = `tree-vis-tag ${d.memberVisibility === "own_dept" ? "tree-vis-own" : "tree-vis-all"}`;
          visTag.textContent = d.memberVisibility === "own_dept" ? "仅本部门" : "可查全部";
          visTag.title = `单部门独立配置：${formatVisLabel(d.memberVisibility)}`;
          row.appendChild(visTag);
        }

        const cnt = document.createElement("span");
        cnt.className = "count-pill";
        cnt.textContent = String(memberCounts.get(d.deptID) || 0);
        row.appendChild(cnt);

        const actions = document.createElement("span");
        actions.className = "tree-row-actions";

        const addSub = document.createElement("button");
        addSub.className = "mini-btn";
        addSub.title = "新建子部门";
        addSub.textContent = "+";
        addSub.addEventListener("click", (e) => {
          e.stopPropagation();
          openDeptModal(null, d.deptID);
        });

        const editBtn = document.createElement("button");
        editBtn.className = "mini-btn";
        editBtn.title = "编辑部门";
        editBtn.textContent = "✎";
        editBtn.addEventListener("click", (e) => {
          e.stopPropagation();
          openDeptModal(d, d.parentID);
        });

        const delBtn = document.createElement("button");
        delBtn.className = "mini-btn danger";
        delBtn.title = "删除部门";
        delBtn.textContent = "×";
        delBtn.addEventListener("click", (e) => {
          e.stopPropagation();
          openDeleteDeptModal(d);
        });

        actions.append(addSub, editBtn, delBtn);
        row.appendChild(actions);

        row.addEventListener("click", () => {
          state.viewMode = "dept";
          state.selectedDeptID = d.deptID;
          renderAll();
        });

        container.appendChild(row);
        if (children.length && !isCollapsed) {
          renderBranch(d.deptID, depth + 1);
        }
      }
    }

    renderBranch("", 0);
  }

  function renderMainPanel() {
    $("deptPanel").classList.add("hidden");
    $("allUsersPanel").classList.add("hidden");
    $("historyPanel").classList.add("hidden");

    if (state.viewMode === "history") {
      $("historyPanel").classList.remove("hidden");
      renderHistoryPanel();
    } else if (state.viewMode === "dept" && state.selectedDeptID && deptById(state.selectedDeptID)) {
      $("deptPanel").classList.remove("hidden");
      renderDeptDetailPanel();
    } else {
      state.viewMode = "users";
      $("allUsersPanel").classList.remove("hidden");
      renderAllUsersPanel();
    }
  }

  function renderEmpNoDisplay(u, inDraft) {
    const inBase = wasInBaseOrg(u.userID);
    if (!inBase && inDraft) {
      return `<span class="badge badge-neutral">应用后自动分配新工号</span>`;
    }
    if (u.employeeNo) {
      return `<span class="badge badge-emp">${escapeHtml(u.employeeNo)}</span>`;
    }
    return "—";
  }

  function renderDeptDetailPanel() {
    const dept = deptById(state.selectedDeptID);
    if (!dept) return;

    $("deptBreadcrumb").textContent = "组织架构 / " + getDeptBreadcrumb(dept.deptID);
    $("deptTitle").textContent = dept.name;

    const deptMembers = state.draft.members.filter((m) => m.deptID === dept.deptID);
    const leaderCount = deptMembers.filter((m) => m.isLeader).length;
    const deptVis = dept.memberVisibility || "";
    const effectiveVis = deptVis || state.draft.memberVisibility || "all";
    const visDesc = deptVis
      ? `${formatVisLabel(effectiveVis)}（本部门单独配置）`
      : `${formatVisLabel(effectiveVis)}（继承组织默认）`;
    $("deptMeta").textContent = `成员 ${deptMembers.length} 人 · 主管 ${leaderCount} 人 · 查看权限：${visDesc}`;
    const deptVisSel = $("deptVisibilitySelect");
    if (deptVisSel) {
      deptVisSel.options[0].textContent = `继承组织默认（${formatDeptVisShort(state.draft.memberVisibility || "all")}）`;
      deptVisSel.value = deptVis;
    }

    const kw = state.memberSearch.trim().toLowerCase();
    const rows = deptMembers
      .map((m) => ({ m, u: effectiveUser(m.userID) }))
      .filter(({ u }) => {
        if (!u) return false;
        if (!kw) return true;
        return (
          (u.nickname || "").toLowerCase().includes(kw) ||
          (u.account || "").toLowerCase().includes(kw) ||
          (u.employeeNo || "").toLowerCase().includes(kw) ||
          (u.jobTitle || "").toLowerCase().includes(kw)
        );
      })
      .sort((a, b) => Number(b.m.isLeader) - Number(a.m.isLeader) || (a.u.nickname || a.u.account).localeCompare(b.u.nickname || b.u.account, "zh-CN"));

    const tbody = $("deptMembersBody");
    tbody.innerHTML = "";
    $("deptMembersEmpty").classList.toggle("hidden", rows.length > 0);

    for (const { m, u } of rows) {
      const displayName = u.nickname || u.account || u.userID;
      const tr = document.createElement("tr");
      tr.innerHTML = `
        <td>
          <div class="user-cell">
            <span class="avatar">${escapeHtml(displayName.slice(0, 1).toUpperCase())}</span>
            <div>
              <div><strong>${escapeHtml(displayName)}</strong></div>
              <div class="user-sub">${escapeHtml(u.email || u.userID)}</div>
            </div>
          </div>
        </td>
        <td>${escapeHtml(u.account || "—")}</td>
        <td>${renderEmpNoDisplay(u, true)}</td>
        <td>${escapeHtml(u.jobTitle || "—")}</td>
        <td>
          <span class="badge ${m.isLeader ? "badge-leader" : "badge-neutral"}">${m.isLeader ? "部门主管" : "成员"}</span>
        </td>
        <td class="col-actions"></td>
      `;
      const actTd = tr.querySelector(".col-actions");

      const editJobBtn = document.createElement("button");
      editJobBtn.className = "btn btn-ghost btn-sm";
      editJobBtn.textContent = "职位";
      editJobBtn.addEventListener("click", () => openUserEditModal(u));

      const toggleLeaderBtn = document.createElement("button");
      toggleLeaderBtn.className = "btn btn-ghost btn-sm";
      toggleLeaderBtn.textContent = m.isLeader ? "取消主管" : "设为主管";
      toggleLeaderBtn.addEventListener("click", () => {
        m.isLeader = !m.isLeader;
        renderAll();
      });

      const removeBtn = document.createElement("button");
      removeBtn.className = "btn btn-danger-ghost btn-sm";
      removeBtn.textContent = "移出";
      removeBtn.addEventListener("click", () => {
        const remainingDepts = state.draft.members.filter(
          (x) => x.userID === u.userID && x.deptID !== dept.deptID
        );
        if (remainingDepts.length === 0 && wasInBaseOrg(u.userID)) {
          openRemoveFromOrgModal(u, dept);
        } else {
          state.draft.members = state.draft.members.filter(
            (x) => !(x.deptID === dept.deptID && x.userID === u.userID)
          );
          renderAll();
        }
      });

      actTd.append(editJobBtn, " ", toggleLeaderBtn, " ", removeBtn);
      tbody.appendChild(tr);
    }
  }

  function renderAllUsersPanel() {
    const deptsByUser = new Map();
    for (const m of state.draft.members) {
      if (!deptsByUser.has(m.userID)) deptsByUser.set(m.userID, []);
      deptsByUser.get(m.userID).push(m);
    }

    let inOrgCount = 0;
    for (const u of state.base.users) {
      if ((deptsByUser.get(u.userID) || []).length > 0) inOrgCount++;
    }
    const extCount = state.base.users.length - inOrgCount;
    $("allUsersMeta").textContent = `共 ${state.base.users.length} 位注册用户 · 组织成员 ${inOrgCount} 人（客户端显示组织架构）· 外部用户 ${extCount} 人（客户端不显示组织架构）`;

    const kw = state.allUsersSearch.trim().toLowerCase();
    const list = state.base.users
      .map((raw) => effectiveUser(raw.userID))
      .filter((u) => {
        const mems = deptsByUser.get(u.userID) || [];
        if (state.userFilter === "in_org" && mems.length === 0) return false;
        if (state.userFilter === "external" && mems.length > 0) return false;
        if (!kw) return true;
        const deptNames = mems.map((m) => (deptById(m.deptID) || {}).name || "").join(" ");
        return (
          (u.nickname || "").toLowerCase().includes(kw) ||
          (u.account || "").toLowerCase().includes(kw) ||
          (u.employeeNo || "").toLowerCase().includes(kw) ||
          (u.jobTitle || "").toLowerCase().includes(kw) ||
          deptNames.toLowerCase().includes(kw)
        );
      });

    const tbody = $("allUsersBody");
    tbody.innerHTML = "";
    $("allUsersEmpty").classList.toggle("hidden", list.length > 0);

    for (const u of list) {
      const displayName = u.nickname || u.account || u.userID;
      const mems = deptsByUser.get(u.userID) || [];
      const recs = userEmployeeRecords(u.userID);
      const hasRemovedRec = recs.some((r) => r.status === 2);

      const tr = document.createElement("tr");
      const tagsHtml = mems.length
        ? mems
            .map((m) => {
              const d = deptById(m.deptID);
              const label = (d ? d.name : m.deptID) + (m.isLeader ? " ★主管" : "");
              return `<span class="badge badge-accent">${escapeHtml(label)}</span>`;
            })
            .join("")
        : hasRemovedRec
          ? `<span class="badge badge-removed">已移出组织（工号归档）</span>`
          : `<span class="badge badge-external">无组织架构（外部用户）</span>`;

      const empSub =
        !wasInBaseOrg(u.userID) && mems.length > 0
          ? "应用后自动分配新工号"
          : u.employeeNo
            ? `工号: ${u.employeeNo}${mems.length === 0 ? "（已归档）" : ""}`
            : "尚未加入组织";

      tr.innerHTML = `
        <td>
          <div class="user-cell">
            <span class="avatar">${escapeHtml(displayName.slice(0, 1).toUpperCase())}</span>
            <div>
              <div><strong>${escapeHtml(displayName)}</strong></div>
              <div class="user-sub">${escapeHtml(u.email || u.signature || u.userID)}</div>
            </div>
          </div>
        </td>
        <td>${escapeHtml(u.account || "—")}</td>
        <td>
          <div>${escapeHtml(u.jobTitle || "—")}</div>
          <div class="user-sub">${escapeHtml(empSub)}</div>
        </td>
        <td><div class="tag-list">${tagsHtml}</div></td>
        <td class="col-actions"></td>
      `;
      const editBtn = document.createElement("button");
      editBtn.className = "btn btn-secondary btn-sm";
      editBtn.textContent = "分配部门 / 职位";
      editBtn.addEventListener("click", () => openUserEditModal(u));
      tr.querySelector(".col-actions").appendChild(editBtn);
      tbody.appendChild(tr);
    }
  }

  function renderHistoryPanel() {
    const allRecs = state.base.employeeRecords || [];
    const activeCount = allRecs.filter((r) => r.status === 1).length;
    const removedCount = allRecs.filter((r) => r.status === 2).length;
    $("historyMeta").textContent = `共 ${allRecs.length} 条工号记录 · 在职 ${activeCount} 条 · 已移出归档 ${removedCount} 条（工号永久保留且永不复用）`;

    const kw = state.historySearch.trim().toLowerCase();
    const list = allRecs.filter((r) => {
      if (state.historyFilter === "active" && r.status !== 1) return false;
      if (state.historyFilter === "removed" && r.status !== 2) return false;
      if (!kw) return true;
      const depts = (r.deptNames || []).join(" ");
      return (
        (r.employeeNo || "").toLowerCase().includes(kw) ||
        (r.nickname || "").toLowerCase().includes(kw) ||
        (r.account || "").toLowerCase().includes(kw) ||
        (r.jobTitle || "").toLowerCase().includes(kw) ||
        depts.toLowerCase().includes(kw) ||
        (r.removedReason || "").toLowerCase().includes(kw)
      );
    });

    const tbody = $("historyBody");
    tbody.innerHTML = "";
    $("historyEmpty").classList.toggle("hidden", list.length > 0);

    for (const r of list) {
      const isRemoved = r.status === 2;
      const tr = document.createElement("tr");
      tr.innerHTML = `
        <td><span class="badge badge-emp">${escapeHtml(r.employeeNo)}</span></td>
        <td>
          <div><strong>${escapeHtml(r.nickname || r.account || r.userID)}</strong></div>
          <div class="user-sub">账号: ${escapeHtml(r.account || "—")}</div>
        </td>
        <td>
          <span class="badge ${isRemoved ? "badge-removed" : "badge-accent"}">${isRemoved ? "已移出组织" : "在职"}</span>
        </td>
        <td>
          <div>${escapeHtml((r.deptNames || []).join(" / ") || "—")}</div>
          <div class="user-sub">${escapeHtml(r.jobTitle || "未设职位")}</div>
        </td>
        <td>
          <div class="user-sub">加入：${escapeHtml(formatTime(r.joinedAtMs))}</div>
          ${isRemoved ? `<div class="user-sub">移出：${escapeHtml(formatTime(r.removedAtMs))}</div>` : ""}
        </td>
        <td>
          ${isRemoved ? `<div>${escapeHtml(r.removedReason || "（未填写原因）")}</div>${r.removedByAccount ? `<div class="user-sub">操作人：${escapeHtml(r.removedByAccount)}</div>` : ""}` : `<span class="user-sub">—</span>`}
        </td>
      `;
      tbody.appendChild(tr);
    }
  }

  // ---- Modals ----

  function openModal(title, bodyEl, footerButtons) {
    $("modalTitle").textContent = title;
    const body = $("modalBody");
    body.innerHTML = "";
    body.appendChild(bodyEl);
    const footer = $("modalFooter");
    footer.innerHTML = "";
    for (const btn of footerButtons) footer.appendChild(btn);
    $("modalBackdrop").classList.remove("hidden");
  }

  function closeModal() {
    $("modalBackdrop").classList.add("hidden");
  }

  function makeBtn(text, cls, onClick) {
    const b = document.createElement("button");
    b.type = "button";
    b.className = `btn ${cls}`;
    b.textContent = text;
    b.addEventListener("click", onClick);
    return b;
  }

  function openRemoveFromOrgModal(user, dept) {
    const existingReason = state.draft.removalReasons[user.userID] || "";
    const wrap = document.createElement("div");
    wrap.innerHTML = `
      <p style="margin:0 0 10px;">
        将 <strong>${escapeHtml(user.nickname || user.account)}</strong>（账号 ${escapeHtml(user.account)}${user.employeeNo ? `，工号 ${escapeHtml(user.employeeNo)}` : ""}）从最后一个部门「${escapeHtml(dept.name)}」移出后，该用户将离开组织架构。
      </p>
      <div class="diff-box" style="margin-bottom:12px;">
        <div>• 移出后将保留该用户的原工号与个人信息，但该工号将永久归档不再复用。</div>
        <div>• 若该用户后续再次加入组织，将自动分配全新工号；历史工号与移出原因保留可查。</div>
      </div>
      <div class="field" style="margin-bottom:0;">
        <label for="mRemoveReason">移出原因（可选，创始人与该用户本人可查看）</label>
        <textarea id="mRemoveReason" rows="3" maxlength="300" placeholder="例如：岗位调整、项目结束、离职等（可留空）">${escapeHtml(existingReason)}</textarea>
      </div>
    `;

    openModal("移出组织架构", wrap, [
      makeBtn("取消", "btn-secondary", closeModal),
      makeBtn("确认移出（暂存）", "btn-danger", () => {
        const reason = wrap.querySelector("#mRemoveReason").value.trim();
        state.draft.members = state.draft.members.filter(
          (x) => !(x.deptID === dept.deptID && x.userID === user.userID)
        );
        if (reason) state.draft.removalReasons[user.userID] = reason;
        else delete state.draft.removalReasons[user.userID];
        closeModal();
        renderAll();
      }),
    ]);
    setTimeout(() => wrap.querySelector("#mRemoveReason").focus(), 20);
  }

  function openDeptModal(editingDept, defaultParentID = "") {
    const isEdit = Boolean(editingDept);
    const blockedParents = isEdit ? getDescendantIds(editingDept.deptID) : new Set();
    if (isEdit) blockedParents.add(editingDept.deptID);

    const wrap = document.createElement("div");
    wrap.innerHTML = `
      <div class="field">
        <label>部门名称 *</label>
        <input id="mDeptName" type="text" maxlength="64" placeholder="例如：研发中心" value="${escapeHtml(isEdit ? editingDept.name : "")}" />
      </div>
      <div class="field-row">
        <div class="field">
          <label>上级部门</label>
          <select id="mDeptParent">
            <option value="">（顶级部门）</option>
          </select>
        </div>
        <div class="field">
          <label>排序号（数字越小越靠前）</label>
          <input id="mDeptSort" type="number" value="${isEdit ? Number(editingDept.sortOrder || 0) : state.draft.departments.length}" />
        </div>
      </div>
      <div class="field">
        <label for="mDeptVis">本部门成员查看权限（组织架构树始终全员可见）</label>
        <select id="mDeptVis">
          <option value="">继承组织默认（当前：${escapeHtml(formatVisLabel(state.draft.memberVisibility || "all"))}）</option>
          <option value="all">可查看全部部门成员（单独配置）</option>
          <option value="own_dept">仅可查看本部门成员（单独配置）</option>
        </select>
      </div>
      <div id="mDeptErr" class="alert alert-error hidden"></div>
    `;

    const sel = wrap.querySelector("#mDeptParent");
    const visSel = wrap.querySelector("#mDeptVis");
    visSel.value = isEdit ? editingDept.memberVisibility || "" : "";
    for (const d of state.draft.departments) {
      if (blockedParents.has(d.deptID)) continue;
      const opt = document.createElement("option");
      opt.value = d.deptID;
      opt.textContent = getDeptBreadcrumb(d.deptID);
      sel.appendChild(opt);
    }
    sel.value = isEdit ? editingDept.parentID || "" : defaultParentID || "";

    const save = () => {
      const name = wrap.querySelector("#mDeptName").value.trim();
      const parentID = sel.value;
      const sortOrder = parseInt(wrap.querySelector("#mDeptSort").value, 10) || 0;
      const memberVisibility = visSel.value || "";
      const errEl = wrap.querySelector("#mDeptErr");

      if (!name) {
        errEl.textContent = "部门名称不能为空。";
        errEl.classList.remove("hidden");
        return;
      }
      const dup = state.draft.departments.find(
        (d) =>
          (!isEdit || d.deptID !== editingDept.deptID) &&
          (d.parentID || "") === parentID &&
          d.name.trim().toLowerCase() === name.toLowerCase()
      );
      if (dup) {
        errEl.textContent = `同一上级下已存在名为「${name}」的部门。`;
        errEl.classList.remove("hidden");
        return;
      }

      if (isEdit) {
        editingDept.name = name;
        editingDept.parentID = parentID;
        editingDept.sortOrder = sortOrder;
        editingDept.memberVisibility = memberVisibility;
      } else {
        const newDept = {
          deptID: `tmp_dept_${state.tmpSeq++}`,
          parentID,
          name,
          sortOrder,
          memberVisibility,
        };
        state.draft.departments.push(newDept);
        if (parentID) state.collapsed.delete(parentID);
        state.viewMode = "dept";
        state.selectedDeptID = newDept.deptID;
      }
      closeModal();
      renderAll();
    };

    openModal(isEdit ? "编辑部门" : defaultParentID ? "新建子部门" : "新建顶级部门", wrap, [
      makeBtn("取消", "btn-secondary", closeModal),
      makeBtn("确定", "btn-primary", save),
    ]);
    setTimeout(() => wrap.querySelector("#mDeptName").focus(), 20);
  }

  function openDeleteDeptModal(dept) {
    const descendants = getDescendantIds(dept.deptID);
    const allRemoved = new Set([dept.deptID, ...descendants]);
    const affectedMembers = state.draft.members.filter((m) => allRemoved.has(m.deptID)).length;

    const wrap = document.createElement("div");
    wrap.innerHTML = `
      <p style="margin:0 0 10px;">确认删除部门 <strong>${escapeHtml(dept.name)}</strong>？</p>
      <div class="diff-box">
        ${descendants.size ? `<div>• 将同时删除其下级 <strong>${descendants.size}</strong> 个子部门。</div>` : ""}
        <div>• 将解除 <strong>${affectedMembers}</strong> 条部门成员归属（不会删除用户账号与历史工号）。</div>
        <div>• 若有成员因此退出全部部门，点击「确认应用并通知客户端」时可为其填写移出原因。</div>
      </div>
    `;

    openModal("删除部门", wrap, [
      makeBtn("取消", "btn-secondary", closeModal),
      makeBtn("确认删除", "btn-danger", () => {
        state.draft.departments = state.draft.departments.filter((d) => !allRemoved.has(d.deptID));
        state.draft.members = state.draft.members.filter((m) => !allRemoved.has(m.deptID));
        if (allRemoved.has(state.selectedDeptID)) {
          state.selectedDeptID = state.draft.departments[0]?.deptID || "";
          state.viewMode = state.selectedDeptID ? "dept" : "users";
        }
        closeModal();
        renderAll();
      }),
    ]);
  }

  function openAddMembersModal() {
    const dept = deptById(state.selectedDeptID);
    if (!dept) return;

    const existing = new Set(state.draft.members.filter((m) => m.deptID === dept.deptID).map((m) => m.userID));
    const selected = new Set();

    const wrap = document.createElement("div");
    wrap.innerHTML = `
      <div class="field" style="margin-bottom:8px;">
        <label>向「${escapeHtml(dept.name)}」添加成员（新进入组织的成员应用后自动分配唯一工号，不可修改）</label>
        <input id="mPickSearch" type="search" placeholder="搜索姓名、账号或工号..." />
      </div>
      <div id="mPickList" class="check-list"></div>
    `;

    const listEl = wrap.querySelector("#mPickList");
    const searchInput = wrap.querySelector("#mPickSearch");

    function renderCandidates() {
      const kw = searchInput.value.trim().toLowerCase();
      listEl.innerHTML = "";
      const cands = state.base.users
        .map((r) => effectiveUser(r.userID))
        .filter((u) => !existing.has(u.userID))
        .filter((u) => {
          if (!kw) return true;
          return (
            (u.nickname || "").toLowerCase().includes(kw) ||
            (u.account || "").toLowerCase().includes(kw) ||
            (u.employeeNo || "").toLowerCase().includes(kw)
          );
        });

      if (!cands.length) {
        listEl.innerHTML = `<div class="empty-hint">没有可添加的候选用户。</div>`;
        return;
      }

      for (const u of cands) {
        const curDepts = state.draft.members
          .filter((m) => m.userID === u.userID)
          .map((m) => deptById(m.deptID)?.name)
          .filter(Boolean);
        const item = document.createElement("label");
        item.className = "check-item";
        const cb = document.createElement("input");
        cb.type = "checkbox";
        cb.checked = selected.has(u.userID);
        cb.addEventListener("change", () => {
          if (cb.checked) selected.add(u.userID);
          else selected.delete(u.userID);
        });
        const info = document.createElement("div");
        const statusDesc = curDepts.length
          ? `当前部门：${curDepts.join(" / ")}${u.employeeNo ? ` · 工号 ${u.employeeNo}` : ""}`
          : u.employeeNo
            ? `曾移出组织（原工号 ${u.employeeNo} 已归档，重新加入将分配新工号）`
            : "当前为外部用户（加入后自动分配工号）";
        info.innerHTML = `
          <div><strong>${escapeHtml(u.nickname || u.account)}</strong> <span class="user-sub">(${escapeHtml(u.account)})</span></div>
          <div class="user-sub">${escapeHtml(statusDesc)}</div>
        `;
        item.append(cb, info);
        listEl.appendChild(item);
      }
    }

    searchInput.addEventListener("input", renderCandidates);
    renderCandidates();

    openModal("添加部门成员", wrap, [
      makeBtn("取消", "btn-secondary", closeModal),
      makeBtn("加入部门", "btn-primary", () => {
        for (const uid of selected) {
          state.draft.members.push({
            deptID: dept.deptID,
            userID: uid,
            isLeader: false,
            sortOrder: 0,
          });
          delete state.draft.removalReasons[uid];
        }
        closeModal();
        renderAll();
      }),
    ]);
  }

  function openUserEditModal(user) {
    const curDeptSet = new Set(state.draft.members.filter((m) => m.userID === user.userID).map((m) => m.deptID));
    const curLeaderMap = new Map(state.draft.members.filter((m) => m.userID === user.userID).map((m) => [m.deptID, Boolean(m.isLeader)]));
    const inBase = wasInBaseOrg(user.userID);
    const recs = userEmployeeRecords(user.userID);
    const existingReason = state.draft.removalReasons[user.userID] || "";

    const empDisplay =
      !inBase && curDeptSet.size > 0
        ? "应用后由系统自动分配新工号（不可修改）"
        : user.employeeNo || "尚未加入组织（拉入组织时自动分配）";

    const historyHtml = recs.length
      ? `
        <div class="field" style="margin-top:12px;margin-bottom:0;">
          <label>历史工号与组织变动记录（${recs.length} 条）</label>
          <div class="history-mini-list">
            ${recs
              .map((r) => {
                const removed = r.status === 2;
                return `
                  <div class="history-mini-item">
                    <div>
                      <span class="badge badge-emp">${escapeHtml(r.employeeNo)}</span>
                      <span class="badge ${removed ? "badge-removed" : "badge-accent"}" style="margin-left:6px;">${removed ? "已移出" : "在职"}</span>
                      <span class="user-sub" style="margin-left:8px;">加入 ${escapeHtml(formatTime(r.joinedAtMs))}${removed ? ` · 移出 ${escapeHtml(formatTime(r.removedAtMs))}` : ""}</span>
                    </div>
                    ${removed ? `<div class="user-sub" style="margin-top:4px;">移出原因：${escapeHtml(r.removedReason || "（未填写）")}</div>` : ""}
                  </div>
                `;
              })
              .join("")}
          </div>
        </div>
      `
      : "";

    const wrap = document.createElement("div");
    wrap.innerHTML = `
      <div class="field-row">
        <div class="field">
          <label>账号（只读）</label>
          <input type="text" value="${escapeHtml(user.account || "")}" disabled />
        </div>
        <div class="field">
          <label>工号（系统自动分配，不可修改）</label>
          <input type="text" value="${escapeHtml(empDisplay)}" disabled />
        </div>
      </div>
      <div class="field">
        <label for="mUJob">职位（仅创始人可修改）</label>
        <input id="mUJob" type="text" maxlength="64" placeholder="例如：客户端架构师、产品经理..." value="${escapeHtml(user.jobTitle || "")}" />
      </div>
      <div class="field-row">
        <div class="field">
          <label>昵称（用户自行维护）</label>
          <input type="text" value="${escapeHtml(user.nickname || "—")}" disabled />
        </div>
        <div class="field">
          <label>邮箱（用户自行维护）</label>
          <input type="text" value="${escapeHtml(user.email || "—")}" disabled />
        </div>
      </div>
      <div class="field-row">
        <div class="field">
          <label>手机号（用户自行维护）</label>
          <input type="text" value="${escapeHtml(user.phone || "—")}" disabled />
        </div>
        <div class="field">
          <label>个性签名（用户自行维护）</label>
          <input type="text" value="${escapeHtml(user.signature || "—")}" disabled />
        </div>
      </div>
      <div class="readonly-hint">说明：工号在拉入组织时由系统自动分配且永不复用；昵称、邮箱、手机号与个性签名由用户在客户端自行修改。</div>
      <div class="field" style="margin-bottom:0;">
        <label>所属部门（不勾选任何部门即为外部用户，客户端不显示组织架构）</label>
        <div id="mUDepts" class="check-list"></div>
      </div>
      <div id="mURemoveReasonWrap" class="reason-box ${inBase && curDeptSet.size === 0 ? "" : "hidden"}">
        <div class="reason-item">
          <label for="mURemoveReason">移出组织原因（可选，保留原工号并归档记录）</label>
          <input id="mURemoveReason" type="text" maxlength="300" placeholder="可填写移出组织原因（留空亦可）" value="${escapeHtml(existingReason)}" />
        </div>
      </div>
      ${historyHtml}
    `;

    const deptListEl = wrap.querySelector("#mUDepts");
    const reasonWrap = wrap.querySelector("#mURemoveReasonWrap");
    const chosenDepts = new Set(curDeptSet);

    const syncReasonVisibility = () => {
      if (inBase && chosenDepts.size === 0) {
        reasonWrap.classList.remove("hidden");
      } else {
        reasonWrap.classList.add("hidden");
      }
    };

    if (!state.draft.departments.length) {
      deptListEl.innerHTML = `<div class="empty-hint">暂未创建部门，请先在左侧创建部门。</div>`;
    } else {
      for (const d of state.draft.departments) {
        const item = document.createElement("label");
        item.className = "check-item";
        const cb = document.createElement("input");
        cb.type = "checkbox";
        cb.checked = chosenDepts.has(d.deptID);
        cb.addEventListener("change", () => {
          if (cb.checked) chosenDepts.add(d.deptID);
          else chosenDepts.delete(d.deptID);
          syncReasonVisibility();
        });
        const label = document.createElement("span");
        label.textContent = getDeptBreadcrumb(d.deptID);
        item.append(cb, label);
        deptListEl.appendChild(item);
      }
    }

    openModal("分配部门与编辑职位", wrap, [
      makeBtn("取消", "btn-secondary", closeModal),
      makeBtn("暂存修改", "btn-primary", () => {
        const orig = state.base.users.find((x) => x.userID === user.userID);
        const nextJob = wrap.querySelector("#mUJob").value.trim();

        if (nextJob !== (orig.jobTitle || "")) {
          state.draft.userUpdates[user.userID] = {
            userID: user.userID,
            jobTitle: nextJob,
          };
        } else {
          delete state.draft.userUpdates[user.userID];
        }

        state.draft.members = state.draft.members.filter((m) => m.userID !== user.userID);
        for (const did of chosenDepts) {
          state.draft.members.push({
            deptID: did,
            userID: user.userID,
            isLeader: curLeaderMap.get(did) || false,
            sortOrder: 0,
          });
        }

        if (inBase && chosenDepts.size === 0) {
          const reason = wrap.querySelector("#mURemoveReason").value.trim();
          if (reason) state.draft.removalReasons[user.userID] = reason;
          else delete state.draft.removalReasons[user.userID];
        } else {
          delete state.draft.removalReasons[user.userID];
        }

        closeModal();
        renderAll();
      }),
    ]);
  }

  function openApplyConfirmModal() {
    const diff = computeDiff();
    if (diff.totalChanges === 0) {
      toast("当前没有待应用的修改。", "info");
      return;
    }
    const wrap = document.createElement("div");
    const items = [];
    if (diff.visibilityChanged) {
      items.push(`组织默认成员查看权限：${formatVisLabel(state.base.memberVisibility || "all")} → ${formatVisLabel(state.draft.memberVisibility || "all")}（组织架构树全员可见）`);
    }
    if (diff.addedDepts.length) items.push(`新增部门（${diff.addedDepts.length}）：${diff.addedDepts.map((d) => d.name).join("、")}`);
    if (diff.modifiedDepts.length) {
      items.push(
        `更新部门/权限（${diff.modifiedDepts.length}）：${diff.modifiedDepts
          .map((x) => `${x.next.name}（${formatDeptVisShort(x.next.memberVisibility || "")}）`)
          .join("、")}`
      );
    }
    if (diff.deletedDepts.length) items.push(`删除部门（${diff.deletedDepts.length}）：${diff.deletedDepts.map((d) => d.name).join("、")}`);
    if (diff.addedMembers) items.push(`新增部门成员归属：${diff.addedMembers} 项`);
    if (diff.removedMembers) items.push(`移出部门成员归属：${diff.removedMembers} 项`);
    if (diff.roleChangedMembers) items.push(`部门主管状态调整：${diff.roleChangedMembers} 项`);
    if (diff.editedUsers.length) items.push(`职位修改：${diff.editedUsers.length} 人`);
    if (diff.newlyJoinedOrg.length) items.push(`加入组织架构：${diff.newlyJoinedOrg.length} 人（将自动分配唯一递增工号 E00001…，不可修改；其客户端实时显示「组织架构」）`);
    if (diff.newlyLeftOrg.length) items.push(`移出组织架构：${diff.newlyLeftOrg.length} 人（保留原工号并永久归档不再复用；其客户端实时隐藏「组织架构」）`);

    const leavingHtml = diff.newlyLeftOrg.length
      ? `
        <div class="reason-box">
          <div style="font-size:12.5px;font-weight:600;color:#991b1b;margin-bottom:8px;">以下成员将移出组织架构，可补充移出原因（可选）：</div>
          ${diff.newlyLeftOrg
            .map((uid) => {
              const u = effectiveUser(uid) || { userID: uid, account: uid };
              const prevReason = state.draft.removalReasons[uid] || "";
              return `
                <div class="reason-item">
                  <label>${escapeHtml(u.nickname || u.account)}（账号 ${escapeHtml(u.account)}${u.employeeNo ? ` · 工号 ${escapeHtml(u.employeeNo)}` : ""}）</label>
                  <input type="text" class="apply-remove-reason" data-uid="${escapeHtml(uid)}" maxlength="300" placeholder="移出原因（可选）" value="${escapeHtml(prevReason)}" />
                </div>
              `;
            })
            .join("")}
        </div>
      `
      : "";

    wrap.innerHTML = `
      <p style="margin:0;">确认将以下组织架构变更提交至服务端并实时通知所有在线客户端？</p>
      <div class="diff-box">
        <ul class="diff-list">${items.map((x) => `<li>${escapeHtml(x)}</li>`).join("")}</ul>
      </div>
      ${leavingHtml}
    `;

    const applyBtn = makeBtn("确认应用并实时推送", "btn-primary", async () => {
      applyBtn.disabled = true;
      applyBtn.textContent = "正在应用...";
      try {
        const removalReasons = {};
        for (const input of wrap.querySelectorAll(".apply-remove-reason")) {
          const uid = input.dataset.uid;
          const val = input.value.trim();
          if (uid && val) {
            removalReasons[uid] = val;
            state.draft.removalReasons[uid] = val;
          }
        }
        const payload = {
          memberVisibility: state.draft.memberVisibility || "all",
          departments: state.draft.departments.map((d) => ({
            deptID: d.deptID,
            parentID: d.parentID || "",
            name: d.name,
            sortOrder: Number(d.sortOrder || 0),
            memberVisibility: d.memberVisibility || "",
          })),
          members: state.draft.members.map((m) => ({
            deptID: m.deptID,
            userID: m.userID,
            isLeader: Boolean(m.isLeader),
            sortOrder: Number(m.sortOrder || 0),
          })),
          userUpdates: Object.values(state.draft.userUpdates),
          removalReasons,
        };
        const res = await api("POST", "/admin/org/apply", payload);
        hydrateSnapshot(res);
        closeModal();
        toast(
          `组织架构已应用（版本 v${res.treeVersion}）${res.notified ? "，已实时推送通知到所有在线客户端" : ""}`,
          "success"
        );
      } catch (err) {
        applyBtn.disabled = false;
        applyBtn.textContent = "确认应用并实时推送";
        toast(err.message || "应用失败", "error");
      }
    });

    openModal("确认应用组织架构变更", wrap, [makeBtn("取消", "btn-secondary", closeModal), applyBtn]);
  }

  function openImportModal() {
    const wrap = document.createElement("div");
    wrap.innerHTML = `
      <p style="margin:0 0 12px;color:var(--text-sec);font-size:13px;">
        上传 JSON 或 XLSX 用户与部门文件（部门路径用 <code>/</code> 分隔，例如 <code>总公司/研发部</code>）。新加入组织的成员将自动分配唯一递增工号，导入完成后实时推送至客户端。
      </p>
      <div class="field">
        <label>选择文件（.json 或 .xlsx）</label>
        <input id="mImportFile" type="file" accept=".json,.xlsx" />
      </div>
      <div id="mImportErr" class="alert alert-error hidden"></div>
    `;

    const importBtn = makeBtn("开始导入并推送", "btn-primary", async () => {
      const file = wrap.querySelector("#mImportFile").files?.[0];
      const errEl = wrap.querySelector("#mImportErr");
      if (!file) {
        errEl.textContent = "请先选择 JSON 或 XLSX 文件。";
        errEl.classList.remove("hidden");
        return;
      }
      const isXlsx = file.name.toLowerCase().endsWith(".xlsx");
      const endpoint = isXlsx ? "/admin/user/import/xlsx" : "/admin/user/import/json";
      const fd = new FormData();
      fd.append("data", file);
      importBtn.disabled = true;
      importBtn.textContent = "导入中...";
      try {
        await api("POST", endpoint, fd, true);
        await loadSnapshot();
        closeModal();
        toast("批量导入成功，并已向客户端推送组织架构更新。", "success");
      } catch (err) {
        importBtn.disabled = false;
        importBtn.textContent = "开始导入并推送";
        errEl.textContent = err.message || "导入失败";
        errEl.classList.remove("hidden");
      }
    });

    openModal("批量导入用户与组织架构", wrap, [makeBtn("取消", "btn-secondary", closeModal), importBtn]);
  }

  // ---- Data loading & auth ----

  function hydrateSnapshot(data) {
    state.orgCode = data.orgCode || "";
    state.serverVersion = data.treeVersion || 0;
    state.base.memberVisibility = data.memberVisibility === "own_dept" ? "own_dept" : "all";
    state.base.departments = data.departments || [];
    state.base.members = data.members || [];
    state.base.users = data.users || [];
    state.base.employeeRecords = data.employeeRecords || [];
    resetDraftFromBase();
    if (state.selectedDeptID && !deptById(state.selectedDeptID)) {
      state.selectedDeptID = state.base.departments[0]?.deptID || "";
    }
    if (!state.selectedDeptID && state.base.departments.length && state.viewMode === "dept") {
      state.selectedDeptID = state.base.departments[0].deptID;
    }
    renderAll();
  }

  async function loadSnapshot() {
    const [me, snap] = await Promise.all([api("GET", "/admin/me"), api("GET", "/admin/org/snapshot")]);
    state.founder = me;
    hydrateSnapshot(snap);
  }

  // ---- Event Wiring ----

  $("loginForm").addEventListener("submit", async (e) => {
    e.preventDefault();
    const account = $("loginAccount").value.trim();
    const password = $("loginPassword").value;
    const btn = $("loginSubmitBtn");
    const errEl = $("loginError");
    errEl.classList.add("hidden");
    btn.disabled = true;
    btn.textContent = "登录中...";
    try {
      const data = await api("POST", "/admin/login", { account, password });
      state.token = data.adminToken;
      sessionStorage.setItem(TOKEN_KEY, state.token);
      await loadSnapshot();
      showApp();
      $("loginPassword").value = "";
      toast("欢迎回来，已进入组织架构管理台。", "success");
    } catch (err) {
      errEl.textContent = err.message || "登录失败";
      errEl.classList.remove("hidden");
    } finally {
      btn.disabled = false;
      btn.textContent = "登录管理台";
    }
  });

  $("btnLogout").addEventListener("click", async () => {
    try {
      await api("POST", "/admin/logout", {});
    } catch {
      // ignore
    }
    logoutLocal();
  });

  $("btnReload").addEventListener("click", async () => {
    const diff = computeDiff();
    if (diff.totalChanges > 0 && !confirm("存在未应用的本地修改，刷新将丢弃这些改动，确认继续？")) return;
    try {
      await loadSnapshot();
      toast("已同步最新服务端数据。", "info");
    } catch (err) {
      toast(err.message, "error");
    }
  });

  $("btnImport").addEventListener("click", openImportModal);
  $("btnDiscard").addEventListener("click", () => {
    resetDraftFromBase();
    renderAll();
    toast("已放弃未应用的修改。", "info");
  });
  $("btnApply").addEventListener("click", openApplyConfirmModal);
  $("btnAddRootDept").addEventListener("click", () => openDeptModal(null, ""));
  $("btnAddSubDept").addEventListener("click", () => openDeptModal(null, state.selectedDeptID));
  $("btnEditDept").addEventListener("click", () => {
    const d = deptById(state.selectedDeptID);
    if (d) openDeptModal(d, d.parentID);
  });
  $("btnDeleteDept").addEventListener("click", () => {
    const d = deptById(state.selectedDeptID);
    if (d) openDeleteDeptModal(d);
  });
  $("btnAddMembers").addEventListener("click", openAddMembersModal);
  $("btnViewAllUsers").addEventListener("click", () => {
    state.viewMode = "users";
    renderAll();
  });
  $("btnViewHistory").addEventListener("click", () => {
    state.viewMode = "history";
    renderAll();
  });

  $("deptSearchInput").addEventListener("input", (e) => {
    state.deptSearch = e.target.value;
    renderDeptTree();
  });
  $("deptMemberSearch").addEventListener("input", (e) => {
    state.memberSearch = e.target.value;
    renderDeptDetailPanel();
  });
  $("allUsersSearch").addEventListener("input", (e) => {
    state.allUsersSearch = e.target.value;
    renderAllUsersPanel();
  });
  $("historySearch").addEventListener("input", (e) => {
    state.historySearch = e.target.value;
    renderHistoryPanel();
  });

  $("userFilterGroup").addEventListener("click", (e) => {
    const btn = e.target.closest(".seg-btn");
    if (!btn) return;
    state.userFilter = btn.dataset.filter;
    for (const b of $("userFilterGroup").querySelectorAll(".seg-btn")) {
      b.classList.toggle("active", b === btn);
    }
    renderAllUsersPanel();
  });

  $("historyFilterGroup").addEventListener("click", (e) => {
    const btn = e.target.closest(".seg-btn");
    if (!btn) return;
    state.historyFilter = btn.dataset.filter;
    for (const b of $("historyFilterGroup").querySelectorAll(".seg-btn")) {
      b.classList.toggle("active", b === btn);
    }
    renderHistoryPanel();
  });

  $("orgVisibilityGroup").addEventListener("click", (e) => {
    const btn = e.target.closest(".seg-btn");
    if (!btn || !btn.dataset.vis) return;
    state.draft.memberVisibility = btn.dataset.vis;
    renderAll();
  });

  $("deptVisibilitySelect").addEventListener("change", (e) => {
    const dept = deptById(state.selectedDeptID);
    if (!dept) return;
    dept.memberVisibility = e.target.value || "";
    renderAll();
  });

  $("modalCloseBtn").addEventListener("click", closeModal);
  $("modalBackdrop").addEventListener("click", (e) => {
    if (e.target === $("modalBackdrop")) closeModal();
  });
  document.addEventListener("keydown", (e) => {
    if (e.key === "Escape") closeModal();
  });

  // Boot
  (async function boot() {
    if (!state.token) {
      showLogin();
      return;
    }
    try {
      await loadSnapshot();
      showApp();
    } catch {
      logoutLocal();
    }
  })();
})();
