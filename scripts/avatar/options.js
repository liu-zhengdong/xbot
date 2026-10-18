// XBot 的 Notionists 选项。HANDOFF 里贴的就是这一份；改这里，gen.js 和出图都跟着变。
// 白名单是逐个部件放大看过的（probe/*.png）：只留平静和微笑，不要皱眉、露牙、张嘴、吐舌、翻白眼、手势。
const CALM = {
  lips: ['variant03', 'variant21', 'variant22', 'variant23'],                          // 03/22/23 微笑，21 浅笑；04/13 平直的嘴配圆点眼看着冷淡，也去掉
  brows: ['variant01', 'variant04', 'variant05', 'variant09', 'variant12'],             // 弧形或平直；去掉 03 一字眉、06/13 八字、07/08 粗斜、10 单挑、11 皱眉、02 下垂
  eyes: ['variant04', 'variant05'],                                                      // 圆点眼；去掉 01/02 半闭眼（不耐烦）、03 眯眼斜看（像翻白眼）
  // 头发：64 种里去掉只有线框、没有黑色块的 3 种（hat、42、61）。它们在 24、16 像素只剩几根细线，认不出是脸
  hair: [
    'variant01', 'variant02', 'variant03', 'variant04', 'variant05', 'variant06', 'variant07', 'variant08', 'variant09', 'variant10',
    'variant11', 'variant12', 'variant13', 'variant14', 'variant15', 'variant16', 'variant17', 'variant18', 'variant19', 'variant20',
    'variant21', 'variant22', 'variant23', 'variant24', 'variant25', 'variant26', 'variant27', 'variant28', 'variant29', 'variant30',
    'variant31', 'variant32', 'variant33', 'variant34', 'variant35', 'variant36', 'variant37', 'variant38', 'variant39', 'variant40',
    'variant41', 'variant43', 'variant44', 'variant45', 'variant46', 'variant47', 'variant48', 'variant49', 'variant50', 'variant51',
    'variant52', 'variant53', 'variant54', 'variant55', 'variant56', 'variant57', 'variant58', 'variant59', 'variant60', 'variant62',
    'variant63',
  ],
  glasses: ['variant03', 'variant08', 'variant11'],                                      // 只要透明镜片；墨镜挡眼睛、心形太闹
  glassesProbability: 20,
  beard: ['variant06', 'variant09', 'variant12'],                                        // 只要细的，不盖住嘴
  beardProbability: 10,
  gestureProbability: 0,                                                                 // 不要手势
  bodyIconProbability: 0,                                                                // 衣服上的小图标，小尺寸是噪点
  backgroundColor: ['transparent'],                                                      // 底色由 CSS 给（浅灰），深色模式不反
};
// 裁切：≥24px 用头肩（PORTRAIT），16px 用只剩脸（FACE）
const PORTRAIT = { scale: 185, translateY: 20 };
const FACE = { scale: 230, translateY: 24, translateX: 4 };
module.exports = { CALM, PORTRAIT, FACE };
