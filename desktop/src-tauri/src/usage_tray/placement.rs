//! Native popup placement. Positions are physical pixels; returned height is dp.

#[cfg(any(windows, test))]
#[derive(Clone, Copy, Default)]
#[repr(C)]
struct Point {
    x: i32,
    y: i32,
}

#[cfg(any(windows, test))]
#[derive(Clone, Copy, Default)]
#[repr(C)]
struct Rect {
    left: i32,
    top: i32,
    right: i32,
    bottom: i32,
}

/// Place a frameless popup next to the taskbar on the cursor's display.
///
/// `width` and `desired_height` are logical dp. The returned x/y are native
/// screen coordinates, for the desktop popup.
pub fn popup_bounds(width: i32, desired_height: i32) -> (i32, i32, i32) {
    #[cfg(windows)]
    if let Some((monitor, work, cursor, dpi)) = native::display_at_cursor() {
        return place(width, desired_height, monitor, work, cursor, dpi);
    }
    // Platforms without a native monitor query still get an accessible origin.
    let _ = width;
    (10, 10, desired_height.max(1))
}

#[cfg(any(windows, test))]
fn physical(dp: i32, dpi: u32) -> i32 {
    ((i64::from(dp.max(0)) * i64::from(dpi) + 95) / 96).min(i64::from(i32::MAX)) as i32
}

#[cfg(any(windows, test))]
fn place(
    width: i32,
    desired_height: i32,
    monitor: Rect,
    work: Rect,
    cursor: Point,
    dpi: u32,
) -> (i32, i32, i32) {
    let dpi = if dpi == 0 { 96 } else { dpi };
    let margin = physical(10, dpi);
    let available_height = (work.bottom - work.top - margin * 2).max(1);
    // Floor the inverse conversion so even a rounded-up physical size fits.
    let height = desired_height
        .max(1)
        .min((i64::from(available_height) * 96 / i64::from(dpi)).max(1) as i32);
    let w = physical(width, dpi);
    let h = physical(height, dpi);
    let min_x = work.left + margin;
    let max_x = (work.right - margin - w).max(min_x);
    let min_y = work.top + margin;
    let max_y = (work.bottom - margin - h).max(min_y);
    let near_edge = physical(48, dpi);

    // Work-area exclusions identify a taskbar even when it is not at the bottom.
    // For auto-hidden taskbars there is no exclusion: use the clicked edge.
    let left = work.left > monitor.left || cursor.x - monitor.left <= near_edge;
    let right = work.right < monitor.right || monitor.right - cursor.x <= near_edge;
    let top = work.top > monitor.top || cursor.y - monitor.top <= near_edge;
    let bottom = work.bottom < monitor.bottom || monitor.bottom - cursor.y <= near_edge;
    let (x, y) = if work.top > monitor.top || (top && !bottom && !left && !right) {
        (cursor.x - w + margin, min_y)
    } else if work.left > monitor.left || (left && !top && !bottom) {
        (min_x, cursor.y - h + margin)
    } else if work.right < monitor.right || (right && !top && !bottom) {
        (max_x, cursor.y - h + margin)
    } else {
        (max_x, max_y)
    };
    (x.clamp(min_x, max_x), y.clamp(min_y, max_y), height)
}

#[cfg(windows)]
mod native {
    use super::{Point, Rect};
    use std::ffi::c_void;

    #[repr(C)]
    struct MonitorInfo {
        size: u32,
        monitor: Rect,
        work: Rect,
        flags: u32,
    }

    #[link(name = "user32")]
    extern "system" {
        fn GetCursorPos(point: *mut Point) -> i32;
        fn MonitorFromPoint(point: Point, flags: u32) -> *mut c_void;
        fn GetMonitorInfoW(monitor: *mut c_void, info: *mut MonitorInfo) -> i32;
    }

    #[link(name = "shcore")]
    extern "system" {
        fn GetDpiForMonitor(monitor: *mut c_void, kind: i32, x: *mut u32, y: *mut u32) -> i32;
    }

    pub(super) fn display_at_cursor() -> Option<(Rect, Rect, Point, u32)> {
        let mut cursor = Point::default();
        let mut info = MonitorInfo {
            size: std::mem::size_of::<MonitorInfo>() as u32,
            monitor: Rect::default(),
            work: Rect::default(),
            flags: 0,
        };
        // Tauri establishes per-monitor DPI awareness before dispatching tray
        // callbacks, so these coordinates and the selected display agree.
        unsafe {
            GetCursorPos(&mut cursor);
            let monitor = MonitorFromPoint(cursor, 2); // MONITOR_DEFAULTTONEAREST
            if monitor.is_null() || GetMonitorInfoW(monitor, &mut info) == 0 {
                return None;
            }
            let (mut x, mut y) = (96, 96);
            let dpi = if GetDpiForMonitor(monitor, 0, &mut x, &mut y) >= 0 && x > 0 {
                x
            } else {
                96
            };
            Some((info.monitor, info.work, cursor, dpi))
        }
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    fn rect(left: i32, top: i32, right: i32, bottom: i32) -> Rect {
        Rect {
            left,
            top,
            right,
            bottom,
        }
    }

    #[test]
    fn bottom_taskbar_stays_inside_work_area_at_each_scale() {
        for dpi in [96, 120, 144, 192] {
            let monitor = rect(1920, 0, 4480, 1440);
            let work = rect(1920, 0, 4480, 1380);
            let (x, y, height) = place(420, 640, monitor, work, Point { x: 4430, y: 1410 }, dpi);
            let margin = physical(10, dpi);
            assert_eq!(height, 640);
            assert_eq!(x, work.right - margin - physical(420, dpi));
            assert_eq!(y, work.bottom - margin - physical(height, dpi));
            assert!(x >= work.left + margin);
        }
    }

    #[test]
    fn negative_coordinate_monitor_and_left_taskbar() {
        let monitor = rect(-1920, -200, 0, 880);
        let work = rect(-1860, -200, 0, 880);
        let (x, y, h) = place(420, 600, monitor, work, Point { x: -1890, y: 790 }, 144);
        assert_eq!(x, -1845);
        assert_eq!(y, -95);
        assert_eq!(h, 600);
    }

    #[test]
    fn top_and_right_taskbars_open_on_the_inside() {
        let monitor = rect(0, 0, 1920, 1080);
        let (x, y, _) = place(
            420,
            600,
            monitor,
            rect(0, 40, 1920, 1080),
            Point { x: 1800, y: 20 },
            120,
        );
        assert_eq!((x, y), (1288, 53));
        let (x, y, _) = place(
            420,
            600,
            monitor,
            rect(0, 0, 1880, 1080),
            Point { x: 1900, y: 1000 },
            96,
        );
        assert_eq!((x, y), (1450, 410));
    }

    #[test]
    fn short_screen_reduces_logical_height_without_overflow() {
        let monitor = rect(0, 0, 1280, 720);
        let work = rect(0, 0, 1280, 680);
        for dpi in [96, 120, 144, 192] {
            let (x, y, height) = place(420, 800, monitor, work, Point { x: 1240, y: 700 }, dpi);
            let margin = physical(10, dpi);
            assert!(height < 800);
            assert!(x >= work.left + margin);
            assert!(y >= work.top + margin);
            assert!(y + physical(height, dpi) <= work.bottom - margin);
        }
    }

    #[test]
    fn auto_hidden_top_taskbar_and_zero_dpi_fallback() {
        let monitor = rect(0, 0, 1920, 1080);
        assert_eq!(
            place(420, 600, monitor, monitor, Point { x: 900, y: 1 }, 0),
            (490, 10, 600)
        );
    }
}
