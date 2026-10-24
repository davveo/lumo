(function () {
  'use strict';
  const row = (title, values, query = title) => ({title, query, items:values.split('/').map(value => {const [label,term] = value.split('~');return {label,query:term || label};})});
  const group = (id, category, label, glyph, rows) => ({id,category,label,title:category,icon:glyph,rows,shortcuts:[{label:'全部'+category,query:''},...rows.slice(0,4).map(value=>({label:value.title,query:value.query}))]});
  const groups = [
    group('appliances','家用电器','冰洗 / 空调 / 厨卫大电','layers',[
      row('冰箱','冰箱/多门冰箱/对开门冰箱/嵌入式冰箱/迷你冰箱/超薄冰箱/十字对开冰箱/法式多门冰箱/风冷冰箱/无霜冰箱/大容量冰箱/酒柜'),
      row('洗衣机','滚筒洗衣机/波轮洗衣机/洗烘套装/洗烘一体机/烘干机/迷你洗衣机/壁挂洗衣机/内衣洗衣机/双筒洗衣机/商用洗衣机'),
      row('空调','壁挂式空调/立式空调/中央空调/移动空调/新风空调/空调配件/变频空调/节能空调/冷暖空调/空调扇'),
      row('厨卫大电','油烟机/燃气灶/集成灶/洗碗机/消毒柜/电热水器/燃气热水器/空气能热水器/壁挂炉/蒸烤一体机/嵌入式蒸箱/集成水槽'),
      row('影音电器','电视/激光电视/投影仪/蓝牙音箱/回音壁/家庭影院/功放/麦克风/音响/播放器'),
      row('中央空调','风管机/多联机/厨房空调/商用柜机/天花机/中央空调面板'),
      row('商用电器','商用冰箱/商用冰柜/展示柜/制冰机/商用洗碗机/商用烤箱/商用消毒柜/商用饮水机'),
      row('家电配件','电视支架/遥控器/空调挡风板/冰箱收纳/洗衣机底座/净水器滤芯/音响线材/热水器配件'),
      row('冷柜冰吧','卧式冷柜/立式冷柜/小型冷柜/冷藏柜/冰吧/双温冷柜')]),
    group('computers','电脑办公','电脑 / 办公 / 文具用品','chip',[
      row('电脑整机','笔记本电脑~笔记本/轻薄本/游戏本/台式电脑/一体机/平板电脑'),
      row('电脑配件','显示器/主板/显卡/处理器/内存/固态硬盘'),
      row('外设产品','键盘/鼠标/鼠标垫/摄像头/拓展坞/移动硬盘'),
      row('办公设备','打印机/复印机/扫描仪/投影仪/碎纸机/标签打印机'),
      row('文具耗材','中性笔/笔记本/文件夹/打印纸/墨盒/硒鼓')]),
    group('digital','手机数码','手机 / 智能 / 摄影数码','camera',[
      row('手机通讯','iPhone~iphone/安卓手机/折叠屏手机/老人手机/游戏手机/手机壳','手机'),
      row('影音娱乐','降噪耳机/无线耳机~耳机/蓝牙耳机/头戴耳机/音箱/音乐播放器','耳机'),
      row('摄影摄像','数码相机~相机/微单相机~微单/单反相机/运动相机/镜头/相机包','相机'),
      row('智能设备','智能手环/智能眼镜/智能音箱/无人机/VR设备/电子阅读器'),
      row('数码配件','充电器/数据线/充电宝/存储卡/手机支架/自拍杆')]),
    group('small-appliances','生活电器','生活电器 / 厨房小电 / 个护','sparkles',[
      row('环境电器','空气净化器/加湿器/除湿机/电风扇/取暖器/新风机'),
      row('清洁电器','扫地机器人/洗地机/吸尘器/蒸汽拖把/除螨仪/擦窗机器人'),
      row('厨房小电','电饭煲/空气炸锅/微波炉/烤箱/破壁机/电压力锅'),
      row('饮水咖啡','咖啡机/电热水壶/养生壶/净水器/饮水机/榨汁机'),
      row('个护电器','吹风机/剃须刀/电动牙刷/冲牙器/卷发棒/按摩仪')]),
    group('food','食品生鲜','食品 / 酒水 / 生鲜特产','bag',[
      row('咖啡茶饮','咖啡豆/挂耳咖啡/速溶咖啡/红茶/绿茶/乌龙茶','咖啡'),
      row('休闲零食','坚果/饼干/巧克力/糖果/肉干/果干'),
      row('粮油调味','大米/面粉/食用油/酱油/调味料/杂粮'),
      row('生鲜冷藏','牛奶/酸奶/水果/蔬菜/肉类/海鲜'),
      row('酒水饮料','白酒/葡萄酒/啤酒/威士忌/果汁/矿泉水')]),
    group('beauty','美妆个护','美妆 / 护肤 / 清洁个护','heart',[
      row('面部护肤','洁面/爽肤水/精华/乳液/面霜/防晒'),
      row('彩妆香氛','香水/口红/粉底/眼影/腮红/定妆粉','香水'),
      row('身体护理','身体乳/沐浴露/护手霜/磨砂膏/身体精油/止汗露'),
      row('洗发护发','洗发水/护发素/发膜/护发精油/染发剂/造型喷雾'),
      row('口腔清洁','牙膏/牙刷/漱口水/牙线/口腔喷雾/假牙清洁')]),
    group('fashion','服饰箱包','男装 / 女装 / 箱包内衣','bag',[
      row('男装','T恤/衬衫/男士外套/牛仔裤/休闲裤/西服'),
      row('女装','连衣裙/女式衬衫/针织衫/半身裙/女士外套/女式牛仔裤'),
      row('功能服饰','防晒衣/羽绒服/保暖内衣/家居服/睡衣/袜子'),
      row('通勤箱包','通勤背包~背包/双肩包~背包/单肩包/手提包/电脑包/公文包','背包'),
      row('出行旅行','行李箱/登机箱/旅行包/收纳包/洗漱包/钱包')]),
    group('sports','运动户外','运动 / 户外 / 骑行健身','shield',[
      row('运动鞋服','运动鞋/跑步鞋/篮球鞋/训练鞋/运动T恤/运动裤','运动'),
      row('健身训练','哑铃/瑜伽垫/跳绳/跑步机/动感单车/筋膜枪'),
      row('球类运动','篮球/足球/羽毛球拍/网球拍/乒乓球拍/高尔夫球'),
      row('户外露营','帐篷/天幕/睡袋/折叠椅/登山杖/户外背包'),
      row('骑行游泳','自行车/骑行头盔/骑行手套/泳衣/泳镜/冲浪板')]),
    group('home','家居生活','家居 / 家纺 / 收纳家具','layers',[
      row('客厅家具','沙发/茶几/电视柜/边几/置物架/休闲椅','沙发'),
      row('卧室家具','床/床垫/床头柜/衣柜/梳妆台/斗柜'),
      row('床上用品','床单/被套/四件套/枕头/被子/凉席'),
      row('居家日用','雨伞/时钟/香薰/垃圾桶/衣架/晾衣架'),
      row('收纳整理','收纳箱/收纳柜/衣物收纳/桌面收纳/鞋架/挂钩')]),
    group('kitchen','厨具餐具','厨具 / 餐具 / 水具茶具','bag',[
      row('烹饪锅具','炒锅/煎锅/汤锅/蒸锅/奶锅/压力锅'),
      row('刀剪砧板','菜刀/水果刀/剪刀/砧板/磨刀器/刀具套装'),
      row('餐具用品','碗/餐盘/筷子/勺子/餐具套装/便当盒'),
      row('水具茶具','保温杯/玻璃杯/水壶/茶壶/茶杯/咖啡杯')]),
    group('renovation','家装建材','家装 / 建材 / 灯具卫浴','layers',[
      row('照明灯饰','吸顶灯/吊灯/台灯/落地灯/筒灯/灯泡'),
      row('厨房卫浴','水槽/龙头/花洒/马桶/浴室柜/智能马桶盖'),
      row('基础建材','地板/瓷砖/墙纸/油漆/门锁/五金配件'),
      row('电工电气','开关/插座/插线板/电线/断路器/智能门铃')]),
    group('baby','母婴玩具','母婴 / 玩具 / 童装乐器','heart',[
      row('喂养用品','奶粉/奶瓶/奶嘴/吸奶器/辅食机/儿童餐具'),
      row('婴儿护理','纸尿裤/湿巾/婴儿洗护/婴儿床/婴儿推车/安全座椅'),
      row('益智玩具','积木/拼图/遥控玩具/毛绒玩具/儿童绘画/桌游'),
      row('童装童鞋','婴儿服/儿童T恤/儿童外套/儿童裤装/学步鞋/儿童运动鞋')]),
    group('pets','宠物生活','宠物 / 主粮 / 清洁用品','heart',[
      row('宠物食品','猫粮/狗粮/宠物罐头/宠物零食/冻干/宠物营养品'),
      row('宠物日用','猫砂/猫砂盆/宠物窝/食盆/饮水机/宠物衣服'),
      row('玩具出行','猫抓板/逗猫棒/宠物球/牵引绳/宠物包/宠物笼'),
      row('宠物清洁','宠物香波/宠物梳/宠物指甲剪/宠物湿巾/除味剂/宠物尿垫')]),
    group('books','图书文娱','图书 / 文娱 / 教育乐器','edit',[
      row('文学阅读','小说/散文/诗歌/传记/经典文学/科幻'),
      row('知识成长','经济管理/历史/哲学/心理学/艺术设计/计算机图书'),
      row('少儿教育','绘本/儿童文学/科普百科/学前教育/教材/字典'),
      row('乐器娱乐','吉他/钢琴/电子琴/尤克里里/口琴/黑胶唱片')]),
    group('jewelry','珠宝钟表','珠宝 / 钟表 / 时尚配饰','clock',[
      row('智能腕表','智能腕表/运动手表/儿童手表/手表表带/手表充电器/手表配件','腕表'),
      row('经典腕表','机械表/石英表/情侣手表/商务手表/怀表/钟表工具'),
      row('珠宝首饰','黄金项链/黄金手镯/钻石戒指/翡翠/珍珠/彩色宝石'),
      row('时尚饰品','耳饰/项链/手链/戒指/胸针/发饰')]),
    group('shoes','鞋靴配饰','男鞋 / 女鞋 / 鞋靴配饰','bag',[
      row('男鞋','男士休闲鞋/男士皮鞋/帆布鞋/男士凉鞋/男靴/男士拖鞋'),
      row('女鞋','高跟鞋/女士平底鞋/女士凉鞋/短靴/长靴/女士拖鞋'),
      row('鞋类配件','鞋垫/鞋带/鞋油/鞋刷/鞋撑/鞋盒'),
      row('穿搭配饰','帽子/围巾/手套/腰带/太阳镜/领带')]),
    group('automotive','汽车用品','汽车 / 养护 / 车载用品','truck',[
      row('汽车养护','机油/玻璃水/燃油添加剂/防冻液/汽车蜡/清洗剂'),
      row('汽车配件','轮胎/蓄电池/雨刷/空调滤芯/空气滤芯/火花塞'),
      row('车载电器','行车记录仪/车载充电器/车载吸尘器/车载冰箱/车载支架/胎压监测'),
      row('内饰清洁','汽车脚垫/座垫/方向盘套/洗车机/洗车毛巾/车载收纳')]),
    group('industrial','工业工具','工具 / 五金 / 劳保用品','chip',[
      row('手动工具','螺丝刀/扳手/钳子/工具箱/锤子/卷尺'),
      row('电动工具','电钻/电锤/角磨机/电锯/热熔胶枪/电动螺丝刀'),
      row('测量工具','激光测距仪/水平仪/万用表/卡尺/温度计/电子秤'),
      row('劳保防护','工作手套/防护眼镜/安全帽/防尘口罩/安全鞋/反光背心')]),
    group('garden','鲜花绿植','鲜花 / 绿植 / 园艺用品','sparkles',[
      row('鲜花礼赠','玫瑰/百合/康乃馨/向日葵/永生花/花束'),
      row('室内绿植','绿萝/多肉/龟背竹/发财树/兰花/盆栽'),
      row('园艺用品','花盆/营养土/肥料/园艺剪/喷壶/园艺手套'),
      row('种子栽培','花卉种子/蔬菜种子/育苗盘/种植箱/支架/水培用品')]),
    group('wellness','健康护理','健康 / 保健 / 居家护理','shield',[
      row('健康监测','血压计/体温计/血氧仪/体脂秤/血糖仪/健康手环'),
      row('日常防护','口罩/消毒液/酒精棉片/创可贴/急救包/防护服'),
      row('运动康复','护膝/护腰/护腕/颈托/冰袋/弹力绷带'),
      row('居家护理','护理垫/拐杖/轮椅/制氧机/雾化器/护理床')])
  ];
  let activeGroup = '', closeTimer;
  const panel = () => document.querySelector('#category-mega-panel');
  const selection = () => ({category:ui.category,query:ui.query || '',label:ui.categoryLabel || '',brand:state.discovery?.filters?.brand || 'all'});
  function choice(label, category, query = '', className = 'category-leaf', brand = '') {
    return `<button type="button" class="${className}" data-category-select="${esc(category)}" data-category-query="${esc(query)}" data-category-label="${esc(label)}"${brand ? ` data-category-brand="${esc(brand)}"` : ''}>${esc(label)}</button>`;
  }
  function groupContent(value) {
    const brands = [...new Set(products.filter(p=>p.visible!==false && p.category===value.category).map(p=>p.brand).filter(Boolean))].slice(0,8);
    return `<div class="category-mega-head"><div><span>精选品类</span><h2>${esc(value.title)}</h2></div><button type="button" class="category-mega-close" data-category-close aria-label="关闭品类面板">${icon('close')}</button></div><div class="category-mega-shortcuts">${value.shortcuts.map(item=>choice(item.label,value.category,item.query,'category-shortcut')).join('')}</div><div class="category-mega-body${brands.length ? '' : ' no-brands'}"><div class="category-mega-rows">${value.rows.map(r=>`<section class="category-mega-row"><button type="button" class="category-row-title" data-category-select="${esc(value.category)}" data-category-query="${esc(r.query)}" data-category-label="${esc(r.title)}">${esc(r.title)}${icon('chevron')}</button><div class="category-row-links">${r.items.map((item,index)=>choice(item.label,value.category,item.query,'category-leaf'+(index===0?' featured':''))).join('')}</div></section>`).join('')}</div>${brands.length ? `<aside class="category-brand-rail" aria-label="相关品牌"><h3>相关品牌</h3><div>${brands.map(brand=>choice(brand,value.category,'','category-brand',brand)).join('')}</div></aside>` : ''}</div>`;
  }
  function directory() {
    return `<aside class="category-directory" aria-label="全部商品分类"><div class="category-directory-head">${icon('layers')}<span>全部商品分类</span><small>${groups.length} 大品类</small></div><div class="category-directory-list">${groups.map(value=>`<button type="button" class="category-directory-item" data-category-group="${value.id}" aria-controls="category-mega-panel" aria-expanded="false">${icon(value.icon)}<span>${esc(value.label)}</span>${icon('chevron')}</button>`).join('')}</div><section id="category-mega-panel" class="category-mega-panel" role="region" aria-label="品类详情" hidden></section></aside>`;
  }
  function show(id) {
    clearTimeout(closeTimer);
    const target = panel(), value = groups.find(item=>item.id===id);
    if (!target || !value) return;
    if (activeGroup!==id || target.hidden) target.innerHTML=groupContent(value);
    activeGroup=id; target.hidden=false;
    for (const button of document.querySelectorAll('[data-category-group]')) {const active=button.dataset.categoryGroup===id;button.classList.toggle('active',active);button.setAttribute('aria-expanded',String(active));}
  }
  function close(restoreFocus = false) {
    clearTimeout(closeTimer);
    if (restoreFocus && activeGroup) document.querySelector(`[data-category-group="${activeGroup}"]`)?.focus();
    const target = panel(); if(target) target.hidden=true;
    for (const button of document.querySelectorAll('[data-category-group]')) {button.classList.remove('active');button.setAttribute('aria-expanded','false');}
    activeGroup='';
  }
  function select(category, query = '', label = '', brand = '') {
    if (!categories.includes(category)) return;
    close();
    if (window.LumoDiscovery?.selectCategory) return window.LumoDiscovery.selectCategory(category,query,label,brand);
    ui.category=category;ui.query=query;ui.categoryLabel=label;ui.onlyFavorites=false;go('');
  }
  function afterRender() { clearTimeout(closeTimer);activeGroup=''; }
  document.addEventListener('mouseover',event=>{const button=event.target.closest?.('[data-category-group]');if(button)show(button.dataset.categoryGroup);if(event.target.closest?.('.category-directory'))clearTimeout(closeTimer);});
  document.addEventListener('mouseout',event=>{if(event.target.closest?.('.category-directory')&&!event.relatedTarget?.closest?.('.category-directory'))closeTimer=setTimeout(()=>close(),140);});
  document.addEventListener('focusin',event=>{const button=event.target.closest?.('[data-category-group]');if(button)show(button.dataset.categoryGroup);else if(!event.target.closest?.('.category-directory'))close();});
  document.addEventListener('pointerdown',event=>{if(!event.target.closest?.('.category-directory'))close();});
  document.addEventListener('click',event=>{
    const target=event.target.closest?.('[data-category-group], [data-category-select], [data-category-close]');if(!target)return;
    if(target.hasAttribute('data-category-close')){close(true);return;}
    if(target.dataset.categoryGroup){show(target.dataset.categoryGroup);return;}
    select(target.dataset.categorySelect,target.dataset.categoryQuery || '',target.dataset.categoryLabel || '',target.dataset.categoryBrand || '');
  });
  document.addEventListener('keydown',event=>{
    if(event.key==='Escape'){close(!!event.target.closest?.('.category-directory'));return;}
    const button=event.target.closest?.('[data-category-group]');
    if(!button)return;
    if(event.key==='ArrowRight'){event.preventDefault();show(button.dataset.categoryGroup);panel()?.querySelector('.category-shortcut')?.focus();return;}
    if(!['ArrowDown','ArrowUp'].includes(event.key))return;
    event.preventDefault();const buttons=[...document.querySelectorAll('[data-category-group]')],index=buttons.indexOf(button),next=buttons[(index+(event.key==='ArrowDown'?1:-1)+buttons.length)%buttons.length];next?.focus();if(next)show(next.dataset.categoryGroup);
  });
  window.addEventListener?.('hashchange',()=>close());
  window.LumoCategories={groups,categories,directory,afterRender,select,close,selection};
})();
