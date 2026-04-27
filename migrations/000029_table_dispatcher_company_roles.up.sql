--
-- PostgreSQL database dump
--

\restrict ZU4qou2A2rZGV9yfIZt6tV5GEX4ltCs9fjHb8xoMiLMAAtZqwin2buSCWVTPZEa

-- Dumped from database version 18.3
-- Dumped by pg_dump version 18.3

SET statement_timeout = 0;
SET lock_timeout = 0;
SET idle_in_transaction_session_timeout = 0;
SET transaction_timeout = 0;
SET client_encoding = 'UTF8';
SET standard_conforming_strings = on;
SELECT pg_catalog.set_config('search_path', '', false);
SET check_function_bodies = false;
SET xmloption = content;
SET client_min_messages = warning;
SET row_security = off;

SET default_tablespace = '';

SET default_table_access_method = heap;

--
-- Name: dispatcher_company_roles; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.dispatcher_company_roles (
    dispatcher_id uuid NOT NULL,
    company_id uuid NOT NULL,
    role character varying(50) NOT NULL,
    invited_by uuid,
    accepted_at timestamp without time zone,
    created_at timestamp without time zone DEFAULT now() NOT NULL
);


--
-- Name: dispatcher_company_roles dispatcher_company_roles_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.dispatcher_company_roles
    ADD CONSTRAINT dispatcher_company_roles_pkey PRIMARY KEY (dispatcher_id, company_id);


--
-- Name: idx_dispatcher_company_roles_company; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_dispatcher_company_roles_company ON public.dispatcher_company_roles USING btree (company_id);


--
-- Name: idx_dispatcher_company_roles_dispatcher; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_dispatcher_company_roles_dispatcher ON public.dispatcher_company_roles USING btree (dispatcher_id);


--
-- Name: dispatcher_company_roles dispatcher_company_roles_company_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.dispatcher_company_roles
    ADD CONSTRAINT dispatcher_company_roles_company_id_fkey FOREIGN KEY (company_id) REFERENCES public.companies(id) ON DELETE CASCADE;


--
-- Name: dispatcher_company_roles fk_dcr_dispatcher; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.dispatcher_company_roles
    ADD CONSTRAINT fk_dcr_dispatcher FOREIGN KEY (dispatcher_id) REFERENCES public.freelance_dispatchers(id) ON DELETE CASCADE;


--
-- PostgreSQL database dump complete
--

\unrestrict ZU4qou2A2rZGV9yfIZt6tV5GEX4ltCs9fjHb8xoMiLMAAtZqwin2buSCWVTPZEa

