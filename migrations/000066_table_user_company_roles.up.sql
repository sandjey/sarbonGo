--
-- PostgreSQL database dump
--


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
-- Name: user_company_roles; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.user_company_roles (
    user_id uuid NOT NULL,
    company_id uuid NOT NULL,
    role_id uuid NOT NULL,
    assigned_by uuid,
    assigned_at timestamp without time zone DEFAULT CURRENT_TIMESTAMP
);


--
-- Name: user_company_roles user_company_roles_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.user_company_roles
    ADD CONSTRAINT user_company_roles_pkey PRIMARY KEY (user_id, company_id, role_id);


--
-- Name: idx_user_company_roles_company; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_user_company_roles_company ON public.user_company_roles USING btree (company_id);


--
-- Name: idx_user_company_roles_user; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_user_company_roles_user ON public.user_company_roles USING btree (user_id);


--
-- Name: user_company_roles user_company_roles_assigned_by_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.user_company_roles
    ADD CONSTRAINT user_company_roles_assigned_by_fkey FOREIGN KEY (assigned_by) REFERENCES public.company_users(id);


--
-- Name: user_company_roles user_company_roles_company_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.user_company_roles
    ADD CONSTRAINT user_company_roles_company_id_fkey FOREIGN KEY (company_id) REFERENCES public.companies(id) ON DELETE CASCADE;


--
-- Name: user_company_roles user_company_roles_role_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.user_company_roles
    ADD CONSTRAINT user_company_roles_role_id_fkey FOREIGN KEY (role_id) REFERENCES public.app_roles(id);


--
-- Name: user_company_roles user_company_roles_user_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.user_company_roles
    ADD CONSTRAINT user_company_roles_user_id_fkey FOREIGN KEY (user_id) REFERENCES public.company_users(id) ON DELETE CASCADE;


--
-- PostgreSQL database dump complete
--


