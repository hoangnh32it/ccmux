# Plan: Import file-browser features from ccmux-master into ccmux-main

Nguồn: `ccmux-master` (Rust, ratatui) có file-tree sidebar + preview
syntax-highlight + mouse resize mà `ccmux-main` (Go) chưa có đầy đủ.
Quyết định phạm vi (đã chốt với user 2026-08-01):

- **Không** làm lại split-pane tự vẽ (PTY/vt100 riêng) — tmux đã làm
  việc này, giữ nguyên triết lý "tmux = session store, ccmux = lens".
- **Có** làm: file-tree cho mọi loại file (không chỉ `.md`), preview
  syntax-highlight, mouse click-focus/drag-resize, cd tracking tự động.
- Người code: Claude Code, qua nhiều phiên làm việc. Ước lượng bên dưới
  tính theo **số phiên agent** (1 phiên ≈ agent code + tự test/build,
  chưa tính review của bạn), kèm quy đổi giờ công tương đương để dễ hình dung.

Điểm thuận lợi đã xác nhận khi khảo sát code: `internal/tui/notes.go`
(1675 dòng) đã có sẵn ~80% logic cần thiết — cây thư mục thu/phóng, ô
preview, tìm kiếm, chuyển project — chỉ giới hạn ở file `.md`. Tận dụng
lại thay vì viết mới. `chroma` (syntax highlight) đã có sẵn as indirect
dep qua Glamour. tmux đã tự track cwd của pane qua
`#{pane_current_path}` — không cần tự parse OSC7 như ccmux-master.

Quy ước dự án: mỗi feature phải lên **openspec change proposal**
trước, và phải có mặt ở cả TUI *và* CLI (feature surface policy trong
CLAUDE.md), kèm test cho từng phần.

Tổng ước lượng: **~10–13 phiên agent** (~28–42 giờ công quy đổi).

Repo: [github.com/hoangnh32it/ccmux](https://github.com/hoangnh32it/ccmux)
(public, khởi tạo 2026-08-01 với baseline commit trước khi bắt đầu
Phase 1). **Quy ước: sau khi hoàn thành mỗi phase bên dưới, commit +
push lên `main` ngay** — mỗi phase là 1 commit riêng, message nêu rõ
phase nào vừa xong, để lịch sử git phản ánh đúng tiến độ tài liệu này.

---

## Phase 0 — Spec & thiết kế (~1 phiên, 2–3h) ✅ DONE 2026-08-01

- [x] Viết openspec proposal (`openspec/changes/file-browser/proposal.md`,
      `design.md`, `tasks.md`, `specs/file-browser-support/spec.md`)
      theo đúng format Why / What Changes / Capabilities như
      `add-grok-agent` (openspec CLI không cài local nên validate bằng
      tay theo template, không chạy được `openspec validate`)
- [x] Quyết định: **màn hình mới** `ScreenFiles` (không generalize
      `notesModel`) — Notes giữ nguyên phạm vi markdown-only, Files là
      package/model riêng (`internal/filebrowser`, `internal/tui/files.go`),
      tái dùng pattern fold/unfold + split-pane từ notes.go
- [x] Vị trí tab: **append cuối cùng** trong `Screen` enum, sau
      `ScreenNetwork` → phím `8`. Lý do: `screenKey()` suy ra số thứ tự
      từ vị trí enum, chèn giữa sẽ đổi số phím của mọi tab phía sau —
      append là lựa chọn an toàn duy nhất (giống cách Grok agent được
      thêm cuối `All()` để giữ thứ tự ổn định)
- [x] Chốt thêm: syntax highlight dùng `chroma` trực tiếp (không qua
      Glamour, Glamour vẫn chỉ dành cho Notes/markdown); cwd tracking
      qua `tmux display-message -p '#{pane_current_path}'` thay vì OSC7
- [x] Tasks.md đã liệt kê chi tiết theo từng phase 2–8 tiếp theo, khớp
      với danh sách trong file kế hoạch này
- [x] Init git repo cho `ccmux-main`, tạo GitHub repo
      `hoangnh32it/ccmux` (public), push baseline commit

## Phase 1 — File-tree cho mọi loại file (~2 phiên, 5–7h) ✅ DONE 2026-08-01

- [x] Tạo package mới `internal/filebrowser` (`filebrowser.go`) walk
      toàn bộ project tree, không lọc theo `.md`, vẫn bỏ qua
      `.git`/`node_modules`/build dirs — `skipDir` giữ **y hệt**
      `internal/notes.skipDir` để Notes và Files thống nhất về "cái gì
      thuộc về project". Chỉ liệt kê regular file (bỏ symlink/socket/
      device — đi theo symlink dir dễ ra ngoài cây hoặc lặp vô hạn).
      Thư mục không đọc được (permission denied) bị skip chứ không làm
      hỏng cả listing
- [x] Tái sử dụng logic thu/phóng thư mục từ `notes.go`, nhưng tách ra
      thành **hàm thuần** trong `tree.go` (`VisibleRows`, `FolderDirs`,
      `DefaultFolds`, `ParentDir`, `DescendantOf`) thay vì method trên
      model — không phụ thuộc Bubble Tea nên test được trực tiếp, và
      Phase 3 chỉ còn việc nối dây
- [x] Thêm binary-file detection (`binary.go`): sniff tối đa 8000 byte
      (ngưỡng của git), coi là binary nếu có byte NUL hoặc không phải
      UTF-8 hợp lệ. Sniff **lười** — chỉ chạy trên file sắp preview,
      không chạy cho từng dòng listing (mở 614 file mỗi lần render là
      không chấp nhận được)
- [x] Unit test cho walk logic + fold/unfold + binary detection
      (table-driven): 30 test, `go test ./...` sạch (48 package)
- [x] Commit + push lên GitHub (`git push origin main`) khi Phase 1 xong

**Ngoài checklist:** `Tree.Resolve` kiểm tra containment (chặn `..`,
đường dẫn tuyệt đối, và symlink trỏ ra ngoài cây) trước khi `Read`.
Phase 6 sẽ nhận `<path>` từ dòng lệnh và — với `--host` — từ mạng, nên
đây là thuộc tính đúng-sai của primitive đọc file, không phải tính năng
thêm.

**Cần quyết định trước Phase 3:** `bin/` không nằm trong `skipDir`
(notes cũng không có), nên khi browse chính repo ccmux thì 3 binary đã
build hiện ra trong cây. Nếu muốn ẩn, thêm `"bin"` vào `skipDir` —
nhưng lúc đó Files sẽ lệch khỏi Notes, và `bin/` là source thật ở một
số project (npm `bin/cli.js`).

## Phase 2 — Preview syntax-highlight (~2 phiên, 5–7h) ✅ DONE 2026-08-01

- [x] Viết renderer dùng `alecthomas/chroma/v2` (`highlight.go`), đã
      promote indirect → direct trong `go.mod`. Style
      **`catppuccin-mocha`** — trùng đúng bảng màu của
      `styles.DefaultPalette`, nên code highlight nằm cùng thế giới màu
      với phần chrome xung quanh thay vì chửi nhau. Formatter
      `terminal256` (không phải `terminal16m`: chroma ghi escape thẳng
      vào string, không đi qua bộ downsample color-profile của lipgloss)
- [x] Map extension → lexer qua `lexers.Match`, không nhận diện được thì
      trả về **content nguyên xi** (không dùng `lexers.Fallback` — nó sẽ
      bọc cả file trong escape mà chẳng tô màu gì)
- [x] Giữ nguyên đường Glamour cho `.md`: `Preview.Render()` trả markdown
      thô để filesModel đưa cho Glamour, chroma cho mọi loại khác
- [x] Giới hạn kích thước: `HighlightLimit` 256 KiB (quá thì plain text)
      và `PreviewLimit` 1 MiB (quá thì chỉ đọc phần đầu, `Truncated=true`).
      Chọn file 2 GB tốn đúng bằng chọn file 2 KB
- [x] Commit + push lên GitHub (`git push origin main`) khi Phase 2 xong

**Kiến trúc:** `Tree.Preview(rel)` làm I/O + phân loại (binary?
truncated? lexer nào? có highlight nổi không?) và trả **text thô**;
chọn renderer là việc của caller. TUI gọi Glamour cho `.md` và
`Highlight` cho phần còn lại, `ccmux files read` in thẳng. Nếu nhét
nhánh rẽ đó vào trong package thì CLI lại phải bóc ANSI ra khỏi output
của chính nó.

**Ghi chú `go mod tidy`:** lệnh này đồng thời sửa 4 dep bị đánh dấu sai
`// indirect` từ trước (`termenv`, `apns2`, `golang.org/x/term`,
`modernc.org/sqlite` — repo dùng trực tiếp cả 4). Đồ thị module không
đổi, chỉ comment dịch chỗ. Kích thước binary **không tăng** vì Glamour
đã kéo sẵn toàn bộ lexer chroma vào rồi.

## Phase 3 — Màn hình TUI mới + wiring (~2 phiên, 5–8h)

- [ ] Thêm `ScreenFiles` vào enum `Screen` trong `app.go`
      (`allScreens()`/`screenLabels` tự động ăn theo, không cần sửa tay
      header)
- [ ] Model `filesModel` (dựa trên khung `notesModel`): 2 pane
      tree/preview, focus switch, scroll
- [ ] Style: dùng token có sẵn trong `internal/tui/styles/` — **không**
      hardcode hex color hay số nguyên padding (bị chặn bởi
      `styles_lint_test.go`)
- [ ] Golden test cho màn hình mới (`teatest`, theo mẫu
      `screens_golden_test.go`)
- [ ] Commit + push lên GitHub (`git push origin main`) khi Phase 3 xong

## Phase 4 — Mouse: click-focus + drag-resize (~1–2 phiên, 4–6h)

- [ ] `app.go` đã có xử lý `tea.MouseMsg` ở tầng router — kiểm tra đã
      hook click-to-focus pane chưa, mở rộng cho vùng tree/preview
      của Files screen
- [ ] Drag border giữa tree và preview để đổi tỉ lệ cột (ccmux-master
      làm bằng cách track `MouseDrag` + so sánh vị trí X) — thêm state
      lưu tỉ lệ, tương tự cách notes.go quản lý `previewPaneSize()`
- [ ] Test tương tác chuột (nếu có harness) hoặc ít nhất unit test cho
      hàm tính lại tỉ lệ cột từ toạ độ chuột
- [ ] Commit + push lên GitHub (`git push origin main`) khi Phase 4 xong

## Phase 5 — cd tracking tự động (~1 phiên, 3–4h)

- [ ] Dùng `tmux display-message -p '#{pane_current_path}'` (qua
      `internal/tmux`) thay vì tự parse OSC7 — tmux đã track sẵn, rẻ
      hơn nhiều so với cách ccmux-master làm
- [ ] Poll hoặc hook vào chu kỳ daemon hiện có để refresh root của
      Files tree khi cwd của pane đang attach thay đổi
- [ ] Test: fake tmux output đổi `pane_current_path`, xác nhận tree
      root cập nhật
- [ ] Commit + push lên GitHub (`git push origin main`) khi Phase 5 xong

## Phase 6 — CLI parity (feature surface policy) (~1 phiên, 2–3h)

- [ ] Thêm subcommand kiểu `ccmux files list|read <project> [--host]`
      mirroring `ccmux notes list|read|search` đã có
- [ ] Cobra command file mới trong `cmd/ccmux/cmd/`
- [ ] Test CLI (giống test hiện có cho notes CLI)
- [ ] Commit + push lên GitHub (`git push origin main`) khi Phase 6 xong

## Phase 7 — Tests tổng hợp + fuzz (nếu áp dụng) (~1 phiên, 3–4h)

- [ ] `go test ./...` sạch trước khi coi là xong (yêu cầu bắt buộc của
      repo)
- [ ] e2e test trong `internal/e2e/` cho CUJ mới: mở Files screen, click
      chọn file, xem preview, resize
- [ ] Cross-compile check `GOOS=windows`, `GOOS=linux` nếu đụng code
      OS-specific (mouse/path handling)
- [ ] Commit + push lên GitHub (`git push origin main`) khi Phase 7 xong

## Phase 8 — Docs (~0.5–1 phiên, 1–2h)

- [ ] Cập nhật `README.md` (mục feature list + có thể thêm 1 GIF demo
      theo `docs/vhs/`)
- [ ] Thêm trang `docs/02_Architecture/0X_File_Browser.md` mô tả kiến
      trúc màn hình mới, theo mẫu `01_Notes_System.md`
- [ ] Cập nhật `CLAUDE.md` phần Components nếu thêm package mới
      (`internal/filebrowser`)
- [ ] Commit + push lên GitHub (`git push origin main`) khi Phase 8 xong

---

## Ghi chú tiến độ

Claude sẽ tự tick `[x]` vào từng dòng trên khi hoàn thành trong các
phiên code tiếp theo, để bạn biết đang ở đâu mà không cần đọc lại toàn
bộ hội thoại.
