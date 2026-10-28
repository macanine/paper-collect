import type { ThemeConfig } from 'antd';

const FONT_FAMILY =
  "-apple-system, BlinkMacSystemFont, 'Segoe UI', Roboto, 'Helvetica Neue', Arial, 'Noto Sans', 'PingFang SC', 'Microsoft YaHei', sans-serif";

/** Ant Design v6 default light theme, with the product's fixed enterprise density. */
export const antTheme: ThemeConfig = {
  token: {
    colorPrimary: '#1677ff',
    colorInfo: '#1677ff',
    colorSuccess: '#52c41a',
    colorWarning: '#faad14',
    colorError: '#ff4d4f',
    colorBgLayout: '#f5f5f5',
    colorBgContainer: '#ffffff',
    colorBgElevated: '#ffffff',
    colorBorder: '#d9d9d9',
    colorBorderSecondary: '#f0f0f0',
    colorText: 'rgba(0, 0, 0, 0.88)',
    colorTextSecondary: 'rgba(0, 0, 0, 0.65)',
    colorTextTertiary: 'rgba(0, 0, 0, 0.45)',
    colorTextQuaternary: 'rgba(0, 0, 0, 0.25)',
    borderRadius: 6,
    borderRadiusLG: 8,
    borderRadiusSM: 4,
    controlHeight: 32,
    fontFamily: FONT_FAMILY,
    fontSize: 14,
    lineHeight: 1.5714285714,
    boxShadowTertiary:
      '0 1px 2px 0 rgba(0,0,0,0.05), 0 1px 6px -1px rgba(0,0,0,0.03), 0 2px 4px 0 rgba(0,0,0,0.03)',
  },
  components: {
    Layout: {
      bodyBg: '#f5f5f5',
      headerBg: '#ffffff',
      siderBg: '#ffffff',
    },
    Button: {
      borderRadius: 6,
      controlHeight: 32,
      fontWeight: 400,
      primaryShadow: 'none',
      defaultShadow: 'none',
      dangerShadow: 'none',
    },
    Card: {
      borderRadiusLG: 8,
      headerBg: '#ffffff',
      headerFontSize: 16,
      headerHeight: 56,
      bodyPadding: 24,
    },
    Input: {
      borderRadius: 6,
      activeShadow: '0 0 0 2px rgba(22, 119, 255, 0.10)',
      hoverBorderColor: '#4096ff',
      activeBorderColor: '#1677ff',
    },
    InputNumber: {
      borderRadius: 6,
      activeShadow: '0 0 0 2px rgba(22, 119, 255, 0.10)',
      hoverBorderColor: '#4096ff',
      activeBorderColor: '#1677ff',
    },
    Select: {
      borderRadius: 6,
      optionSelectedBg: '#e6f4ff',
      optionSelectedColor: '#1677ff',
      hoverBorderColor: '#4096ff',
      activeBorderColor: '#1677ff',
      activeOutlineColor: 'rgba(22, 119, 255, 0.10)',
    },
    Table: {
      headerBg: '#fafafa',
      headerColor: 'rgba(0, 0, 0, 0.88)',
      headerSplitColor: '#f0f0f0',
      borderColor: '#f0f0f0',
      rowHoverBg: '#fafafa',
      cellPaddingBlock: 12,
      cellPaddingInline: 16,
    },
    Menu: {
      itemBg: 'transparent',
      itemSelectedBg: '#e6f4ff',
      itemSelectedColor: '#1677ff',
      itemHoverBg: 'rgba(0, 0, 0, 0.04)',
      itemBorderRadius: 6,
      itemHeight: 40,
    },
    Modal: {
      borderRadiusLG: 8,
      contentBg: '#ffffff',
      headerBg: '#ffffff',
    },
    Drawer: {
      colorBgElevated: '#ffffff',
    },
    Tag: {
      borderRadiusSM: 4,
    },
    Tabs: {
      itemSelectedColor: '#1677ff',
      inkBarColor: '#1677ff',
    },
    Breadcrumb: {
      itemColor: 'rgba(0, 0, 0, 0.45)',
      lastItemColor: 'rgba(0, 0, 0, 0.88)',
      linkColor: 'rgba(0, 0, 0, 0.45)',
      linkHoverColor: '#1677ff',
      separatorColor: 'rgba(0, 0, 0, 0.25)',
    },
    Descriptions: {
      labelBg: '#fafafa',
    },
  },
};
