(function () {
  'use strict';
  const esc=value=>String(value??'').replace(/[&<>"']/g,char=>({'&':'&amp;','<':'&lt;','>':'&gt;','"':'&quot;',"'":'&#39;'}[char]));
  const block=(style='line')=>`<span class="sk-block sk-${style}"></span>`;
  const lines=()=>`${block('line')}${block('line short')}`;
  const product=()=>`<div class="sk-product">${block('media')}<div class="sk-product-info">${lines()}<div class="sk-price-row">${block('price')}${block('circle small')}</div></div></div>`;
  const row=()=>`<div class="sk-row">${block('thumbnail')}<div class="sk-row-copy">${lines()}${block('line tiny')}</div>${block('price')}</div>`;
  const card=()=>`<div class="sk-card">${block('circle')}${lines()}${block('price')}</div>`;
  const coupon=()=>`<div class="sk-coupon"><div>${block('value')}${block('line tiny')}</div><div>${lines()}${block('button')}</div></div>`;
  const form=()=>`<div class="sk-form">${Array.from({length:4},()=>`<div>${block('line tiny')}${block('input')}</div>`).join('')}</div>`;
  const shapes={
    products:count=>`<div class="product-grid sk-product-grid">${Array.from({length:count},product).join('')}</div>`,
    cards:count=>`<div class="sk-card-grid">${Array.from({length:count},card).join('')}</div>`,
    coupons:count=>`<div class="sk-coupon-grid">${Array.from({length:count},coupon).join('')}</div>`,
    rows:count=>`<div class="sk-rows">${Array.from({length:count},row).join('')}</div>`,
    table:count=>`<div class="sk-table">${block('table-head')}${Array.from({length:count},()=>`<div class="sk-table-row">${block('line')}${block('line')}${block('line short')}${block('button')}</div>`).join('')}</div>`,
    detail:()=>`<div class="sk-detail"><div>${block('media')}${block('line short')}</div><div class="sk-detail-copy">${block('title')}${lines()}${block('value')}${form()}<div class="sk-actions">${block('button')}${block('button')}</div></div></div>`,
    profile:()=>`<div class="sk-profile"><div class="sk-profile-head">${block('circle large')}<div>${block('title')}${lines()}</div></div><div class="sk-card-grid">${Array.from({length:4},card).join('')}</div><div class="sk-profile-panels">${form()}<div>${row()}${row()}</div></div></div>`,
    form:()=>form(),
    reviews:count=>`<div class="sk-reviews">${Array.from({length:count},()=>`<div class="sk-review"><div>${block('circle')}${block('line short')}</div>${lines()}${block('line')}</div>`).join('')}</div>`,
    navigation:count=>`<div class="sk-navigation">${Array.from({length:count},()=>`<div>${block('circle small')}${block('line')}</div>`).join('')}</div>`,
    summary:count=>count>1?`<div class="sk-card-grid sk-summary-grid" style="--sk-columns:${Math.min(count,4)}">${Array.from({length:count},()=>`<div class="sk-card">${block('line short')}${block('value')}</div>`).join('')}</div>`:`<div class="sk-summary">${block('title')}${Array.from({length:4},()=>`<div>${block('line short')}${block('price')}</div>`).join('')}${block('button')}</div>`
  };
  function render(type='rows',options={}) {
    if(!Object.hasOwn(shapes,type))type='rows';
    const raw=Number(options.count??(['summary','detail','profile','form'].includes(type)?1:type==='rows'||type==='reviews'?3:6)),count=Number.isFinite(raw)?Math.min(12,Math.max(1,Math.trunc(raw))):6;
    const className=String(options.className||'').split(/\s+/).filter(value=>/^[a-zA-Z][a-zA-Z0-9_-]{0,63}$/.test(value)).join(' ');
    return `<div class="sk-loading sk-loading-${type}${className?' '+className:''}" role="status" aria-live="polite" aria-busy="true"><span class="sk-status">${esc(options.label||'正在加载内容，请稍候')}</span><div aria-hidden="true">${shapes[type](count)}</div></div>`;
  }
  window.LumoSkeleton={render};
})();
