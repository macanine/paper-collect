import type { ThemeConfig } from 'antd';

const FONT_FAMILY =
  "-apple-system, BlinkMacSystemFont, 'Segoe UI', Roboto, 'Helvetica Neue', Arial, 'Noto Sans', 'Noto Sans SC', 'PingFang SC', 'Microsoft YaHei', sans-serif";

/** Ant Design v6 default light theme, aligned with https://ant.design/design.md. */
export const antTheme: ThemeConfig = {
  token: {
    colorPrimary: '#1677FF',
    colorInfo: '#1677FF',
    colorSuccess: '#52C41A',
    colorWarning: '#FAAD14',
    colorError: '#FF4D4F',
    colorLink: '#1677FF',
    colorBgLayout: '#F5F5F5',
    colorBgContainer: '#FFFFFF',
    colorBgElevated: '#FFFFFF',
    colorBorder: '#D9D9D9',
    colorBorderSecondary: '#F0F0F0',
    colorText: '#1F1F1F',
    colorTextSecondary: '#595959',
    colorTextTertiary: '#8C8C8C',
    colorTextQuaternary: '#BFBFBF',
    borderRadius: 6,
    borderRadiusLG: 8,
    borderRadiusSM: 4,
    controlHeight: 32,
    fontFamily: FONT_FAMILY,
    fontSize: 14,
    lineHeight: 1.5714285714,
    boxShadowTertiary:
      '0 1px 2px 0 rgba(0,0,0,0.05), 0 1px 6px -1px rgba(0,0,0,0.03), 0 2px 4px 0 rgba(0,0,0,0.03)',
    boxShadowSecondary:
      '0 6px 16px 0 rgba(0,0,0,0.08), 0 3px 6px -4px rgba(0,0,0,0.12), 0 9px 28px 8px rgba(0,0,0,0.05)',
  },
  components: {
    Layout: {
      bodyBg: '#F5F5F5',
      headerBg: '#FFFFFF',
      siderBg: '#FFFFFF',
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
      headerBg: '#FFFFFF',
      headerFontSize: 16,
      headerHeight: 56,
      bodyPadding: 24,
    },
    Input: {
      borderRadius: 6,
      activeShadow: '0 0 0 2px rgba(5,145,255,0.10)',
      hoverBorderColor: '#4096FF',
      activeBorderColor: '#1677FF',
    },
    InputNumber: {
      borderRadius: 6,
      activeShadow: '0 0 0 2px rgba(5,145,255,0.10)',
      hoverBorderColor: '#4096FF',
      activeBorderColor: '#1677FF',
    },
    Select: {
      borderRadius: 6,
      optionSelectedBg: '#E6F4FF',
      optionSelectedColor: '#1677FF',
      hoverBorderColor: '#4096FF',
      activeBorderColor: '#1677FF',
      activeOutlineColor: 'rgba(5,145,255,0.10)',
    },
    Table: {
      headerBg: '#FAFAFA',
      headerColor: '#1F1F1F',
      headerSplitColor: '#F0F0F0',
      borderColor: '#F0F0F0',
      rowHoverBg: '#FAFAFA',
      cellPaddingBlock: 12,
      cellPaddingInline: 16,
    },
    Menu: {
      itemBg: 'transparent',
      itemSelectedBg: '#E6F4FF',
      itemSelectedColor: '#1677FF',
      itemHoverBg: '#FAFAFA',
      itemBorderRadius: 6,
      itemHeight: 40,
    },
    Modal: {
      borderRadiusLG: 8,
      contentBg: '#FFFFFF',
      headerBg: '#FFFFFF',
    },
    Drawer: {
      colorBgElevated: '#FFFFFF',
    },
    Tag: {
      borderRadiusSM: 4,
    },
    Tabs: {
      itemSelectedColor: '#1677FF',
      inkBarColor: '#1677FF',
    },
    Breadcrumb: {
      itemColor: '#8C8C8C',
      lastItemColor: '#1F1F1F',
      linkColor: '#8C8C8C',
      linkHoverColor: '#1677FF',
      separatorColor: '#BFBFBF',
    },
    Descriptions: {
      labelBg: '#FAFAFA',
    },
  },
};
