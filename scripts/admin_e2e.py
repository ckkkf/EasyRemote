"""管理端 E2E 共用交互（ADM-UI-11c）。

shadcn Select 是 Radix 组件：触发器是 button[role=combobox]，选项是 div[role=option]，
原生 page.select_option 对它无效。这里统一成 pick()，并依赖选项上的 data-value。

用法：
    from admin_e2e import pick
    pick(page, "状态筛选", "closed")
"""


def _trigger(page, label: str):
    trig = page.locator(f'[role=combobox][aria-label="{label}"]')
    if trig.count() == 0:
        trig = page.locator(f'button[aria-label="{label}"]')
    first = trig.first
    first.wait_for(state="visible", timeout=20000)
    # 页面在请求中会把控件置灰：等到可用再点，避免 30s 的 "element is not enabled"。
    page.wait_for_function(
        "(sel) => { const el = document.querySelector(sel); return !!el && !el.disabled; }",
        arg=f'[role=combobox][aria-label="{label}"]',
        timeout=20000,
    )
    return first


def pick(page, label: str, value: str) -> None:
    """选中 shadcn Select 的某个选项（按 data-value）。"""
    _trigger(page, label).click()
    option = page.locator(f'[role=option][data-value="{value}"]').first
    option.wait_for(state="visible", timeout=15000)
    option.click()


def pick_text(page, label: str, text: str) -> None:
    """按选项可见文字选中（少数选项没有稳定 value 时使用）。"""
    _trigger(page, label).click()
    page.get_by_role("option", name=text, exact=True).first.click()
