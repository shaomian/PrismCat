# 上游代码移植辅助：把 cherry-pick 进来的代码中的上游模块路径
# 改写为本仓库路径（模块改名后，上游新增文件携带的自身 import
# 需要修整才能编译）。
#
# 用法：git cherry-pick <上游提交> 之后运行本脚本，然后 go build ./... 验证
$old = "github.com/paopaoandlingyia/PrismCat"
$new = "github.com/shaomian/PrismCat"

$files = Get-ChildItem -Recurse -Filter "*.go" -File | Where-Object {
    $_.FullName -notmatch "\.tools|node_modules"
}
$changed = 0
foreach ($f in $files) {
    $c = [System.IO.File]::ReadAllText($f.FullName)
    if ($c.Contains($old)) {
        [System.IO.File]::WriteAllText($f.FullName, $c.Replace($old, $new), [System.Text.UTF8Encoding]::new($false))
        $changed++
        Write-Host "fixed: $($f.FullName.Replace($PWD.Path + '\', ''))"
    }
}
Write-Host ""
if ($changed -gt 0) {
    Write-Host "$changed file(s) updated. Run 'go build ./...' to verify."
} else {
    Write-Host "no upstream module paths found - nothing to fix."
}